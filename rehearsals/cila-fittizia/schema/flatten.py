"""Flatten an XSD form (InfoCamere Moduli_PA) into leaf fields: path, type, choice, options, comment.

  git clone --depth 1 https://github.com/InfoCamereSCpA/Sistema-Informatico-degli-Sportelli-Unici-Moduli-PA modulipa
  python3 -I flatten.py modulipa/Moduli_PA/SUAP/04_forms/mod_cila_v1.0.7.xsd > fields.json

Loads every XSD of the set (sections name abstract types whose concrete versions live in
the entity files) and expands an abstract type into its subtypes as alternatives. An
xsd:choice gives its fields one shared `choice` id: one of them is the answer.
mod_cila_v1.0.7-fields.json is its output for the CILA, minus the other forms' attachment
lists (the CILA uses the SUE list `allegatiSCIA`).
"""
import json, sys, os
import xml.etree.ElementTree as ET

XS = "{http://www.w3.org/2001/XMLSchema}"
schemas = {}   # namespace -> (root, nsmap, path)

def load(path):
    path = os.path.normpath(path)
    nsmap = {}
    for ev, (p, uri) in ET.iterparse(path, events=["start-ns"]):
        nsmap.setdefault(p, uri)
    root = ET.parse(path, parser=ET.XMLParser(target=ET.TreeBuilder(insert_comments=True))).getroot()
    tns = root.get("targetNamespace", "")
    if tns in schemas:
        return
    schemas[tns] = (root, nsmap, path)
    for imp in root.findall(XS + "import") + root.findall(XS + "include"):
        loc = imp.get("schemaLocation")
        if loc:
            load(os.path.join(os.path.dirname(path), loc))

def resolve(qname, ns_ctx, kind):
    if qname is None:
        return None, None
    prefix, _, local = qname.rpartition(":")
    uri = schemas[ns_ctx][1].get(prefix, "") if prefix else ns_ctx
    if uri == "http://www.w3.org/2001/XMLSchema":
        return ("builtin", local), None
    if uri not in schemas:
        return ("unresolved", qname), None
    root = schemas[uri][0]
    for t in root.findall(XS + kind):
        if t.get("name") == local:
            return t, uri
    return ("unresolved", qname), None

out = []

def comment_before(parent, node):
    prev = None
    for ch in parent:
        if ch is node:
            break
        if ch.tag is ET.Comment:
            prev = ch.text.strip()
        elif isinstance(ch.tag, str):
            prev = None
    return prev

def simple_info(st, ns):
    if st is None or isinstance(st, tuple):
        return st[1] if isinstance(st, tuple) else None, []
    r = st.find(XS + "restriction")
    if r is None:
        return st.get("name"), []
    enums = [e.get("value") for e in r.findall(XS + "enumeration")]
    if not enums and r.get("base"):
        b, bns = resolve(r.get("base"), ns, "simpleType")
        if isinstance(b, ET.Element):
            return simple_info(b, bns)
    return st.get("name") or r.get("base"), enums

def walk_particles(node, ns, path, choice, depth):
    for ch in node:
        if not isinstance(ch.tag, str):
            continue
        tag = ch.tag[len(XS):]
        if tag == "sequence" or tag == "all":
            walk_particles(ch, ns, path, choice, depth)
        elif tag == "choice":
            cid = f"{'/'.join(path)}#choice{len(out)}"
            walk_particles(ch, ns, path, cid, depth)
        elif tag == "element":
            element(ch, node, ns, path, choice, depth)
        elif tag == "group" and ch.get("ref"):
            g, gns = resolve(ch.get("ref"), ns, "group")
            if isinstance(g, ET.Element):
                walk_particles(g, gns, path, choice, depth)
            else:
                unresolved.add(ch.get("ref"))

subtypes = {}  # (uri, name) of a base -> [(ctype, uri)]
unresolved = set()

def index_subtypes():
    for uri, (root, nsmap, _) in schemas.items():
        for ct in root.findall(XS + "complexType"):
            for ext in ct.iter(XS + "extension"):
                b = ext.get("base", "")
                pre, _, loc = b.rpartition(":")
                buri = nsmap.get(pre, "") if pre else uri
                subtypes.setdefault((buri, loc), []).append((ct, uri))

def walk_ctype(ct, ns, path, choice, depth):
    if ct.get("abstract") == "true":
        subs = subtypes.get((ns, ct.get("name")), [])
        if not subs:
            out.append({"path": "/".join(path), "type": "abstract-no-subtype:" + str(ct.get("name")), "choice": choice})
        cid = f"{'/'.join(path)}#subtype"
        for sub, suri in subs:
            out.append({"path": "/".join(path) + "[" + sub.get("name") + "]", "type": "group", "choice": cid, "note": "subtype"})
            walk_ctype_concrete(sub, suri, path + ["[" + sub.get("name") + "]"], cid, depth + 1)
        return
    walk_ctype_concrete(ct, ns, path, choice, depth)

def walk_ctype_concrete(ct, ns, path, choice, depth):
    cc = ct.find(XS + "complexContent")
    if cc is not None:
        ext = cc.find(XS + "extension")
        if ext is None:
            ext = cc.find(XS + "restriction")
        base, bns = resolve(ext.get("base"), ns, "complexType")
        if isinstance(base, ET.Element):
            if base.get("abstract") != "true":
                walk_ctype_concrete(base, bns, path, choice, depth)
        else:
            unresolved.add(ext.get("base"))
        walk_particles(ext, ns, path, choice, depth)
        return
    sc = ct.find(XS + "simpleContent")
    if sc is not None:
        out.append({"path": "/".join(path), "type": "simpleContent", "choice": choice})
        return
    walk_particles(ct, ns, path, choice, depth)

def element(el, parent, ns, path, choice, depth):
    if depth > 25:
        return
    if el.get("ref"):
        target, tns = resolve(el.get("ref"), ns, "element")
        if isinstance(target, ET.Element):
            return element(target, schemas[tns][0], tns, path, choice, depth)
        out.append({"path": "/".join(path + [el.get("ref")]), "type": "unresolved-ref", "choice": choice})
        return
    name = el.get("name")
    p = path + [name]
    rep = el.get("maxOccurs") not in (None, "1")
    opt = el.get("minOccurs") == "0"
    note = comment_before(parent, el)
    ct = el.find(XS + "complexType")
    tns = ns
    st = el.find(XS + "simpleType")
    if ct is None and st is None and el.get("type"):
        t, tns = resolve(el.get("type"), ns, "complexType")
        if isinstance(t, ET.Element):
            ct = t
        else:
            t2, tns2 = resolve(el.get("type"), ns, "simpleType")
            st = t2 if isinstance(t2, ET.Element) else t
            tns = tns2 or ns
    if ct is not None:
        if rep or note:
            out.append({"path": "/".join(p), "type": "group", "repeat": rep, "optional": opt, "choice": choice, "note": note})
        walk_ctype(ct, tns, p, choice, depth + 1)
    else:
        tname, enums = simple_info(st, tns) if not isinstance(st, tuple) else (st[1], [])
        out.append({"path": "/".join(p), "type": tname, "options": enums, "repeat": rep, "optional": opt, "choice": choice, "note": note})

import glob
load(sys.argv[1])
for f in sorted(glob.glob(os.path.join(os.path.dirname(sys.argv[1]), "..", "**", "*.xsd"), recursive=True)):
    load(f)
index_subtypes()
root, nsmap, _ = schemas[ET.parse(sys.argv[1]).getroot().get("targetNamespace")]
for el in root.findall(XS + "element"):
    element(el, root, root.get("targetNamespace"), [], None, 0)
json.dump(out, sys.stdout, indent=1, ensure_ascii=False)
print("unresolved:", sorted(unresolved), file=sys.stderr)
