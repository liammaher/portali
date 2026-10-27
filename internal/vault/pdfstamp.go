package vault

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// Placement says where a signature goes: a 1-based page, the box of the
// printed signature line as [x0, top, x1, bottom] in PDF points from the
// page's top-left (as the library's layouts measure), and optionally the box
// of the date line beside it.
type Placement struct {
	Page     int         `json:"page"`
	Rect     [4]float64  `json:"rect"`
	DateRect *[4]float64 `json:"date_rect,omitempty"`
}

// stampTimeout bounds work on one hostile PDF.
const stampTimeout = 30 * time.Second

func init() {
	// pdfcpu would write a config folder under the user's home on first use;
	// the vault wants no state outside its own folder, and only core fonts.
	api.DisableConfigDir()
}

const (
	// maxInflated caps what a PDF's zlib streams may expand to in total. A
	// 1 MB file can hold a gigabyte of zeros, and the stamper would decode it.
	maxInflated = 64 << 20
	// MaxSigningPages keeps a request within what the Touch ID prompt shows.
	MaxSigningPages    = 1000
	maxSignaturePixels = 8 << 20 // an image bomb stays small in the file
	minPlacementWidth  = 20.0
	minPlacementHeight = 10.0
	// edgeSlack lets a box measured on the page edge run a point past it.
	edgeSlack = 1.0
)

// stampSignature draws the signature image (and date, where a placement has a
// date box) onto pdf and returns the new file and its page count. It refuses
// what it can't place exactly: encrypted files, rotated pages, a crop box
// that differs from the media box, boxes off the page.
func stampSignature(pdf, img []byte, date string, placements []Placement) (out []byte, pages int, err error) {
	// pdfcpu works on hostile input: a panic in it is a refusal, not a crash.
	defer func() {
		if r := recover(); r != nil {
			out, pages, err = nil, 0, &ValidationError{Field: "pdf", Msg: "not a PDF this vault can read"}
		}
	}()
	if len(placements) == 0 {
		return nil, 0, &ValidationError{Field: "placements", Msg: "at least one signature box is required"}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxSignaturePixels {
		return nil, 0, &ValidationError{Field: "signature", Msg: "the stored signature image can't be used"}
	}

	if err := checkInflation(pdf); err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), stampTimeout)
	defer cancel()
	conf := model.NewDefaultConfiguration()
	pdfCtx, err := api.ReadContext(ctx, bytes.NewReader(pdf), conf)
	if err != nil {
		return nil, 0, &ValidationError{Field: "pdf", Msg: "not a PDF this vault can read (encrypted or damaged files are refused)"}
	}
	if pdfCtx.Encrypt != nil {
		return nil, 0, &ValidationError{Field: "pdf", Msg: "an encrypted PDF can't be signed: save an unencrypted copy"}
	}
	// Validating builds the page tree the box checks below read.
	if err := api.ValidateContext(ctx, pdfCtx); err != nil {
		return nil, 0, &ValidationError{Field: "pdf", Msg: "not a PDF this vault can read (encrypted or damaged files are refused)"}
	}
	pages = pdfCtx.PageCount
	if pages < 1 || pages > MaxSigningPages {
		return nil, 0, &ValidationError{Field: "pdf", Msg: fmt.Sprintf("must have 1 to %d pages", MaxSigningPages)}
	}

	marks := map[int][]*model.Watermark{}
	for i, p := range placements {
		field := fmt.Sprintf("placements[%d]", i)
		if p.Page < 1 || p.Page > pages {
			return nil, 0, &ValidationError{Field: field, Msg: fmt.Sprintf("page %d is not in a PDF of %d pages", p.Page, pages)}
		}
		_, _, attrs, err := pdfCtx.PageDict(ctx, p.Page, false)
		if err != nil || attrs == nil || attrs.MediaBox == nil {
			return nil, 0, &ValidationError{Field: field, Msg: fmt.Sprintf("can't read the size of page %d", p.Page)}
		}
		mb := attrs.MediaBox
		if attrs.Rotate%360 != 0 || mb.LL.X != 0 || mb.LL.Y != 0 ||
			(attrs.CropBox != nil && *attrs.CropBox != *mb) {
			return nil, 0, &ValidationError{Field: field, Msg: fmt.Sprintf("page %d is rotated or cropped, so a box can't be placed on it exactly", p.Page)}
		}
		pageW, pageH := mb.Width(), mb.Height()

		if err := checkBox(field+".rect", p.Rect, pageW, pageH); err != nil {
			return nil, 0, err
		}
		wm, err := imageMark(ctx, img, cfg, p.Rect, pageH)
		if err != nil {
			return nil, 0, err
		}
		marks[p.Page] = append(marks[p.Page], wm)

		if p.DateRect != nil {
			if err := checkBox(field+".date_rect", *p.DateRect, pageW, pageH); err != nil {
				return nil, 0, err
			}
			wm, err := dateMark(ctx, date, *p.DateRect, pageH, conf)
			if err != nil {
				return nil, 0, err
			}
			marks[p.Page] = append(marks[p.Page], wm)
		}
	}

	var stamped bytes.Buffer
	if err := api.AddWatermarksSliceMap(ctx, bytes.NewReader(pdf), &stamped, marks, conf); err != nil {
		return nil, 0, &ValidationError{Field: "pdf", Msg: "the signature could not be placed on this PDF"}
	}
	return stamped.Bytes(), pages, nil
}

// checkInflation refuses a PDF whose zlib streams (content, images, object
// streams) expand past maxInflated in total, before anything decodes them.
// It looks at each "stream ... endstream" that starts with a zlib header; a
// stream it can't read is left to the PDF reader.
func checkInflation(pdf []byte) error {
	budget := int64(maxInflated)
	rest := pdf
	for {
		i := bytes.Index(rest, []byte("stream"))
		if i < 0 {
			return nil
		}
		after := rest[i+len("stream"):]
		if bytes.HasSuffix(rest[:i], []byte("end")) {
			rest = after
			continue
		}
		switch {
		case bytes.HasPrefix(after, []byte("\r\n")):
			after = after[2:]
		case bytes.HasPrefix(after, []byte("\n")):
			after = after[1:]
		default:
			rest = after
			continue
		}
		end := bytes.Index(after, []byte("endstream"))
		body := after
		if end >= 0 {
			body = after[:end]
		}
		if len(body) > 2 && body[0]&0x0f == 8 && (int(body[0])<<8|int(body[1]))%31 == 0 {
			if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
				n, _ := io.Copy(io.Discard, io.LimitReader(zr, budget+1))
				budget -= n
				if budget < 0 {
					return &ValidationError{Field: "pdf", Msg: "its compressed streams expand to more than 64 MB"}
				}
			}
		}
		if end < 0 {
			return nil
		}
		rest = after[end+len("endstream"):]
	}
}

// checkBox refuses a box that is not finite, not ordered, too small to hold
// a signature, or off the page.
func checkBox(field string, r [4]float64, pageW, pageH float64) error {
	for _, v := range r {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return &ValidationError{Field: field, Msg: "must be four numbers [x0, top, x1, bottom]"}
		}
	}
	if r[2]-r[0] < minPlacementWidth || r[3]-r[1] < minPlacementHeight {
		return &ValidationError{Field: field, Msg: "is too small (a box needs at least 20 x 10 points, ordered [x0, top, x1, bottom])"}
	}
	if r[0] < -edgeSlack || r[1] < -edgeSlack || r[2] > pageW+edgeSlack || r[3] > pageH+edgeSlack {
		return &ValidationError{Field: field, Msg: "is not on the page"}
	}
	return nil
}

// imageMark fits the image inside r, centred, keeping its proportions.
func imageMark(ctx context.Context, img []byte, cfg image.Config, r [4]float64, pageH float64) (*model.Watermark, error) {
	w, h := r[2]-r[0], r[3]-r[1]
	scale := math.Min(w/float64(cfg.Width), h/float64(cfg.Height))
	dx := r[0] + (w-float64(cfg.Width)*scale)/2
	dy := pageH - r[3] + (h-float64(cfg.Height)*scale)/2
	desc := fmt.Sprintf("pos:bl, off:%.2f %.2f, scale:%.6f abs, rot:0, op:1", dx, dy, scale)
	wm, err := api.ImageWatermarkForReader(ctx, bytes.NewReader(img), desc, true, false, types.POINTS)
	if err != nil {
		return nil, &ValidationError{Field: "signature", Msg: "the stored signature image can't be placed"}
	}
	return wm, nil
}

// dateMark writes the date in Helvetica, as large as the box allows (11 pt at
// most), vertically centred.
func dateMark(ctx context.Context, date string, r [4]float64, pageH float64, conf *model.Configuration) (*model.Watermark, error) {
	w, h := r[2]-r[0], r[3]-r[1]
	// pdfcpu takes whole points.
	points := math.Floor(math.Min(11, math.Min(h*0.8, w/(float64(len(date))*0.56))))
	if points < 5 {
		return nil, &ValidationError{Field: "date_rect", Msg: "is too small for the date"}
	}
	dy := pageH - r[3] + (h-points)/2
	desc := fmt.Sprintf("pos:bl, off:%.2f %.2f, points:%.0f, scale:1 abs, rot:0, op:1, fillc:0 0 0, font:Helvetica", r[0], dy, points)
	wm, err := api.TextWatermark(ctx, strings.TrimSpace(date), desc, true, false, types.POINTS, conf)
	if err != nil {
		return nil, &ValidationError{Field: "date_rect", Msg: "the date could not be placed"}
	}
	return wm, nil
}

// minimalPDF is a blank 200 x 100 point page, used to try a signature image
// before it is stored.
var minimalPDF = buildMinimalPDF()

func buildMinimalPDF() []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}
