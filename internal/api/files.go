package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/lovincyrus/personal-vault/internal/vault"
)

// The file index (vault/files.go). Reading and suggesting follow the caller's
// scope (files.*, leo:files.*) and tier cap; a sync client is a service token
// with write access. Linking, rules, sources, suggestions' acceptance and
// pruning are the user's: the session or a grant they approved for that scope.

// maxSyncBatch keeps a sync request inside the 1 MB body limit.
const maxSyncBatch = 500

// userRead refuses anyone but the user: the session or a grant they approved
// in person. Which rows it then shows is the vault's filter (callerFilter).
func userRead(w http.ResponseWriter, r *http.Request) bool {
	if !isUser(r) {
		sessionRequired(w)
		return false
	}
	return true
}

// userWrite is userRead for a change: the grant must also allow writing and
// reach each holder's files, as RemovePerson asks of a grant for a person.
func userWrite(w http.ResponseWriter, r *http.Request, holders ...string) bool {
	if !userRead(w, r) {
		return false
	}
	ids := make([]string, len(holders))
	for i, h := range holders {
		ids[i] = vault.FilesScopeID(h)
	}
	return requireAccess(w, r, true) && requireScope(w, r, ids...)
}

// holderOf turns a person reference (io for the owner) into a holder.
func holderOf(w http.ResponseWriter, ref string) (string, bool) {
	if ref == "" || ref == vault.OwnerRef {
		return "", true
	}
	if !vault.ValidPersonSlug(ref) {
		writeError(w, http.StatusBadRequest, "invalid_request", "for must be io or a person's slug")
		return "", false
	}
	return ref, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if !requireJSON(w, r) {
		return false
	}
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return false
	}
	return true
}

// canIndex reports a caller that may read or write the index as a whole:
// its scope reaches the owner's files and its cap reaches sensitive.
func canIndex(w http.ResponseWriter, r *http.Request) bool {
	if !requireScope(w, r, vault.FilesScopeID("")) {
		return false
	}
	if !vault.TierAllows(maxTierFromRequest(r), "sensitive") {
		sensitivityDenied(w)
		return false
	}
	return true
}

// GET /vault/files?q=&for=&pratica=&source=&provider=&unlinked=1&gone=1&limit=
func (s *Server) handleSearchFiles(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fq := vault.FileQuery{
		Text: q.Get("q"), Pratica: q.Get("pratica"), SourceID: q.Get("source"), Provider: q.Get("provider"),
		Unlinked: q.Get("unlinked") == "1", IncludeGone: q.Get("gone") == "1",
	}
	if q.Has("for") {
		h, ok := holderOf(w, q.Get("for"))
		if !ok {
			return
		}
		fq.Holder = &h
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive number")
			return
		}
		fq.Limit = n
	}
	files, total, err := s.vault.SearchFiles(callerFilter(r), fq)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if files == nil {
		files = []vault.FileInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "total": total})
}

// GET /vault/files/{id}
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	f, err := s.vault.GetFileInfo(callerFilter(r), r.PathValue("id"))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type syncRequest struct {
	SourceID string                  `json:"source_id"`
	SyncID   string                  `json:"sync_id,omitempty"` // names a longer sync; finish it with /vault/files/sync/finish
	Begin    bool                    `json:"begin,omitempty"`   // the first batch of that sync
	Confirm  bool                    `json:"confirm,omitempty"` // finish: yes, most of the source really is gone
	Full     bool                    `json:"full,omitempty"`    // the batch is the whole source (no sync_id)
	Files    []vault.FileObservation `json:"files"`
}

// POST /vault/files/sync: what a sync client saw at a source. A source too
// big for one request is sent as batches that share a sync_id, then finished.
func (s *Server) handleSyncFiles(w http.ResponseWriter, r *http.Request) {
	if !requireAccess(w, r, true) || !canIndex(w, r) {
		return
	}
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Files) > maxSyncBatch {
		writeError(w, http.StatusBadRequest, "invalid_request", "send at most "+strconv.Itoa(maxSyncBatch)+" files at a time")
		return
	}
	if req.Full && req.SyncID != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "full and sync_id don't go together")
		return
	}
	var res *vault.FileSyncResult
	var err error
	if req.SyncID != "" {
		res, err = s.vault.UpsertFileBatchIf(callerFilter(r), req.SourceID, req.SyncID, req.Begin, req.Files)
	} else {
		res, err = s.vault.UpsertFilesIf(callerFilter(r), req.SourceID, req.Files, req.Full)
	}
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /vault/files/sync/finish
func (s *Server) handleFinishFileSync(w http.ResponseWriter, r *http.Request) {
	if !requireAccess(w, r, true) || !canIndex(w, r) {
		return
	}
	var req syncRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.vault.FinishFileSync(callerFilter(r), req.SourceID, req.SyncID, req.Confirm)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /vault/files/prune
func (s *Server) handlePruneFiles(w http.ResponseWriter, r *http.Request) {
	if !userWrite(w, r, "") {
		return
	}
	n, err := s.vault.PruneFiles()
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"pruned": n})
}

// GET /vault/files/sources
func (s *Server) handleListFileSources(w http.ResponseWriter, r *http.Request) {
	if !canIndex(w, r) {
		return
	}
	srcs, err := s.vault.FileSources(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if srcs == nil {
		srcs = []vault.FileSourceInfo{}
	}
	writeJSON(w, http.StatusOK, srcs)
}

type addSourceRequest struct {
	Provider string `json:"provider"`
	vault.FileSourceConfig
}

// POST /vault/files/sources
func (s *Server) handleAddFileSource(w http.ResponseWriter, r *http.Request) {
	if !userWrite(w, r, "") {
		return
	}
	var req addSourceRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	src, err := s.vault.AddFileSource(req.Provider, req.FileSourceConfig)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, src)
}

// DELETE /vault/files/sources/{id}
func (s *Server) handleForgetFileSource(w http.ResponseWriter, r *http.Request) {
	// It erases every person's links to the source's files.
	if !userWrite(w, r, "", vault.AnyHolder) {
		return
	}
	n, err := s.vault.ForgetFileSource(r.PathValue("id"))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"forgotten": n})
}

// POST /vault/files/sources/{id}/state: a sync finished; keep its cursor.
func (s *Server) handleFileSourceState(w http.ResponseWriter, r *http.Request) {
	if !requireAccess(w, r, true) || !canIndex(w, r) {
		return
	}
	var req struct {
		Cursor string `json:"cursor"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.vault.SetFileSourceState(r.PathValue("id"), req.Cursor); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /vault/files/{id}/links
func (s *Server) handleLinkFile(w http.ResponseWriter, r *http.Request) {
	var in vault.FileLinkInput
	if !userWrite(w, r) || !decodeJSON(w, r, &in) {
		return
	}
	var ok bool
	if in.Holder, ok = holderOf(w, in.Holder); !ok || !userWrite(w, r, in.Holder) {
		return
	}
	link, err := s.vault.LinkFile(r.PathValue("id"), in, vault.FileOriginManual)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// DELETE /vault/files/links/{id}
func (s *Server) handleUnlinkFile(w http.ResponseWriter, r *http.Request) {
	if !userWrite(w, r) {
		return
	}
	if err := s.vault.UnlinkFile(callerFilter(r), r.PathValue("id")); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

// GET /vault/files/rules[?source=id]
func (s *Server) handleListFileRules(w http.ResponseWriter, r *http.Request) {
	if !userRead(w, r) {
		return
	}
	rules, err := s.vault.FileRules(callerFilter(r), r.URL.Query().Get("source"))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if rules == nil {
		rules = []vault.FileRuleInfo{}
	}
	writeJSON(w, http.StatusOK, rules)
}

type addRuleRequest struct {
	SourceID string `json:"source_id"`
	vault.FileRuleInput
}

// POST /vault/files/rules
func (s *Server) handleAddFileRule(w http.ResponseWriter, r *http.Request) {
	var req addRuleRequest
	if !userWrite(w, r) || !decodeJSON(w, r, &req) {
		return
	}
	var ok bool
	if req.Holder, ok = holderOf(w, req.Holder); !ok || !userWrite(w, r, req.Holder) {
		return
	}
	rule, linked, err := s.vault.AddFileRule(req.SourceID, req.FileRuleInput)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": rule, "linked": linked})
}

// DELETE /vault/files/rules/{id}
func (s *Server) handleRemoveFileRule(w http.ResponseWriter, r *http.Request) {
	if !userWrite(w, r) {
		return
	}
	if err := s.vault.RemoveFileRule(callerFilter(r), r.PathValue("id")); err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// GET /vault/files/suggestions
func (s *Server) handleListFileSuggestions(w http.ResponseWriter, r *http.Request) {
	if !userRead(w, r) {
		return
	}
	sugs, err := s.vault.FileSuggestions(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if sugs == nil {
		sugs = []vault.FileSuggestionInfo{}
	}
	writeJSON(w, http.StatusOK, sugs)
}

// POST /vault/files/{id}/suggest: an agent proposes a link.
func (s *Server) handleSuggestFileLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		vault.FileLinkInput
		Reason string `json:"reason"`
	}
	if !requireAccess(w, r, false) || !decodeJSON(w, r, &req) {
		return
	}
	var ok bool
	if req.Holder, ok = holderOf(w, req.Holder); !ok {
		return
	}
	if !requireScope(w, r, vault.FilesScopeID(req.Holder)) {
		return
	}
	sug, err := s.vault.SuggestFileLink(callerFilter(r), r.PathValue("id"), req.FileLinkInput, consumerFromRequest(r), req.Reason)
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sug)
}

// POST /vault/files/suggestions/{id}/accept|reject
func (s *Server) handleResolveFileSuggestion(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "accept" && action != "reject" {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if !userWrite(w, r) {
		return
	}
	link, err := s.vault.ResolveFileSuggestion(callerFilter(r), r.PathValue("id"), action == "accept")
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": action + "ed", "link": link})
}

// POST /vault/files/proposals: an agent proposes a file its own connector
// found (Claude's Google Drive, say). It enters the index only when the user
// accepts.
func (s *Server) handleProposeFile(w http.ResponseWriter, r *http.Request) {
	var in vault.FileProposalInput
	if !requireAccess(w, r, false) || !decodeJSON(w, r, &in) {
		return
	}
	// "for": "io" says the owner; that is a request for a link even though the
	// holder it becomes is empty.
	asked := in.Holder != ""
	var ok bool
	if in.Holder, ok = holderOf(w, in.Holder); !ok {
		return
	}
	if asked && in.Kind == "" && in.Pratica == "" {
		in.Kind = vault.FileKindPerson
	}
	if !vault.ScopeAllows(scopeFromRequest(r), vault.FilesScopeID(in.Holder)) {
		scopeDenied(w)
		return
	}
	p, err := s.vault.ProposeFile(callerFilter(r), in, consumerFromRequest(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// GET /vault/files/proposals
func (s *Server) handleListFileProposals(w http.ResponseWriter, r *http.Request) {
	if !userRead(w, r) {
		return
	}
	list, err := s.vault.FileProposals(callerFilter(r))
	if err != nil {
		handleVaultError(w, err)
		return
	}
	if list == nil {
		list = []vault.FileProposalInfo{}
	}
	writeJSON(w, http.StatusOK, list)
}

// POST /vault/files/proposals/{id}/accept|reject
func (s *Server) handleResolveFileProposal(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "accept" && action != "reject" {
		writeError(w, http.StatusNotFound, "not_found", "not found")
		return
	}
	if !userWrite(w, r) {
		return
	}
	f, err := s.vault.ResolveFileProposal(callerFilter(r), r.PathValue("id"), action == "accept")
	if err != nil {
		handleVaultError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": action + "ed", "file": f})
}
