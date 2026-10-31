#!/usr/bin/env python3
"""Generate the README's blueprint-style SVG diagrams.

Usage, from the repository root: python3 tools/readme-diagrams/gen.py docs/assets
"""
import sys
from xml.sax.saxutils import escape

FONT = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"
TEXT = "#e8f1ff"
BG = "#103f73"
NODE_FS, NODE_CW, NODE_LH = 13, 7.9, 17
LBL_FS, LBL_CW, LBL_LH = 11, 6.7, 14

CATS = {
    "start":   ("#7dd3fc", "#7dd3fc", 0.20, False),
    "check":   ("#5eead4", "#2dd4bf", 0.18, False),
    "draw":    ("#c4b5fd", "#a78bfa", 0.20, False),
    "publish": ("#fde68a", "#facc15", 0.16, False),
    "wrote":   ("#fdba74", "#fb923c", 0.20, False),
    "fail":    ("#f9a8d4", "#f472b6", 0.20, False),
    "stopped": ("#cbd5e1", "#94a3b8", 0.20, True),
}


class SVG:
    def __init__(self, w, h, title, desc):
        self.w, self.h = w, h
        self.title, self.desc = title, desc
        self.groups, self.edges, self.nodes, self.labels = [], [], [], []

    # --- primitives -------------------------------------------------------
    def node(self, cx, y, text, cat, w=None):
        lines = text.split("\n")
        if w is None:
            w = round(max(len(l) for l in lines) * NODE_CW + 26)
        h = len(lines) * NODE_LH + 16
        stroke, fill, op, dashed = CATS[cat]
        x = cx - w / 2
        dash = ' stroke-dasharray="5 3"' if dashed else ""
        out = [f'<rect x="{x:.1f}" y="{y}" width="{w}" height="{h}" rx="3" '
               f'fill="{fill}" fill-opacity="{op}" stroke="{stroke}" stroke-width="1.5"{dash}/>']
        ty = y + 8 + NODE_LH * 0.78
        for i, l in enumerate(lines):
            out.append(f'<text x="{cx}" y="{ty + i * NODE_LH:.1f}" text-anchor="middle" '
                       f'font-size="{NODE_FS}">{escape(l)}</text>')
        self.nodes.append("\n".join(out))
        return dict(cx=cx, x=x, y=y, w=w, h=h, top=y, bottom=y + h, left=x, right=x + w, cy=y + h / 2)

    def group(self, x, y, w, h, title, anchor="start"):
        tx = {"start": x + 14, "middle": x + w / 2, "end": x + w - 14}[anchor]
        self.groups.append(
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="#ffffff" fill-opacity="0.03" '
            f'stroke="#b9d3f2" stroke-opacity="0.85" stroke-width="1.2" stroke-dasharray="6 4"/>\n'
            f'<line x1="{x}" y1="{y + 30}" x2="{x + w}" y2="{y + 30}" stroke="#b9d3f2" '
            f'stroke-opacity="0.85" stroke-width="1.2" stroke-dasharray="6 4"/>\n'
            f'<text x="{tx}" y="{y + 20}" text-anchor="{anchor}" font-size="13" '
            f'font-weight="600">{escape(title)}</text>')

    def edge(self, d, label=None, at=None, dashed=False):
        dash = ' stroke-dasharray="5 4"' if dashed else ""
        self.edges.append(f'<path d="{d}" fill="none" stroke="{TEXT}" stroke-opacity="0.9" '
                          f'stroke-width="1.3"{dash} marker-end="url(#arrow)"/>')
        if label:
            self.label(at[0], at[1], label)

    def label(self, cx, cy, text):
        lines = text.split("\n")
        w = round(max(len(l) for l in lines) * LBL_CW + 12)
        h = len(lines) * LBL_LH + 6
        x, y = cx - w / 2, cy - h / 2
        out = [f'<rect x="{x:.1f}" y="{y:.1f}" width="{w}" height="{h}" rx="2" fill="#0c2f57" '
               f'stroke="#b9d3f2" stroke-opacity="0.75" stroke-width="0.8"/>']
        for i, l in enumerate(lines):
            out.append(f'<text x="{cx}" y="{y + 3 + LBL_LH * 0.8 + i * LBL_LH:.1f}" '
                       f'text-anchor="middle" font-size="{LBL_FS}">{escape(l)}</text>')
        self.labels.append("\n".join(out))

    def start_dot(self, cx, cy):
        self.nodes.append(f'<circle cx="{cx}" cy="{cy}" r="6" fill="#7dd3fc"/>')

    def legend(self, x, y, w, items):
        h = 80
        self.groups.append(
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" fill="none" stroke="#b9d3f2" '
            f'stroke-opacity="0.85" stroke-width="1.2" stroke-dasharray="6 4"/>\n'
            f'<line x1="{x}" y1="{y + 28}" x2="{x + w}" y2="{y + 28}" stroke="#b9d3f2" '
            f'stroke-opacity="0.85" stroke-width="1.2" stroke-dasharray="6 4"/>\n'
            f'<text x="{x + w / 2}" y="{y + 19}" text-anchor="middle" font-size="12">Legend</text>')
        widths = [round(len(t) * NODE_CW + 26) for t, _ in items]
        gap = (w - 24 - sum(widths)) / (len(items) - 1)
        cx = x + 12
        for (t, cat), iw in zip(items, widths):
            self.node(cx + iw / 2, y + 40, t, cat, w=iw)
            cx += iw + gap

    # --- output -----------------------------------------------------------
    def render(self):
        w, h = self.w, self.h
        return f'''<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" role="img" aria-labelledby="t d">
<title id="t">{escape(self.title)}</title>
<desc id="d">{escape(self.desc)}</desc>
<defs>
<pattern id="minor" width="20" height="20" patternUnits="userSpaceOnUse"><path d="M 20 0 L 0 0 0 20" fill="none" stroke="#7fb0e6" stroke-opacity="0.13" stroke-width="0.6"/></pattern>
<pattern id="major" width="100" height="100" patternUnits="userSpaceOnUse"><rect width="100" height="100" fill="url(#minor)"/><path d="M 100 0 L 0 0 0 100" fill="none" stroke="#9cc3ee" stroke-opacity="0.26" stroke-width="1"/></pattern>
<marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M 0 1 L 9 5 L 0 9 z" fill="{TEXT}"/></marker>
</defs>
<rect width="{w}" height="{h}" fill="{BG}"/>
<rect width="{w}" height="{h}" fill="url(#major)"/>
<g font-family="{FONT}" fill="{TEXT}">
<text x="{w / 2}" y="42" text-anchor="middle" font-size="24">{escape(self.heading)}</text>
{chr(10).join(self.groups)}
{chr(10).join(self.edges)}
{chr(10).join(self.nodes)}
{chr(10).join(self.labels)}
</g>
</svg>
'''


def curve(x1, y1, x2, y2):
    """Vertical S-curve from (x1,y1) down/up to (x2,y2)."""
    my = (y1 + y2) / 2
    return f"M {x1} {y1} C {x1} {my} {x2} {my} {x2} {y2}"


# ---------------------------------------------------------------------------
# One track
# ---------------------------------------------------------------------------
def track():
    s = SVG(1020, 1600,
            "How amdl downloads one track",
            "Statechart of one track: resolving metadata and standalone covers; then for each codec in "
            "download.quality_priority, followed by aac-lc when codec_alternative is on, a fetch phase "
            "(choose the web enhancedHls manifest, the wrapper's device manifest for Hi-Res ALAC when "
            "alac_max_sample_rate is above 48 kHz or sets no ceiling, or webplayback for aac-lc; claim the output path; fetch the embedded cover and lyrics, once per track; download the encrypted segments) "
            "and a decrypt phase (decrypt in-process with internal/fairplay, remux with ffmpeg, verify "
            "when check_integrity is on, tag, move into downloads_dir). Each phase has its own "
            "download.max_attempts budget; when one runs out the next codec is tried. End states: "
            "completed, skipped_existing, failed and cancelled.")
    s.heading = "one track, from link to file"
    s.legend(30, 60, 960, [("resolve", "start"), ("choose", "check"), ("fetch & decrypt", "draw"),
                           ("finish the file", "publish"), ("completed", "wrote"), ("failed", "fail"),
                           ("skipped / cancelled", "stopped")])
    MX = 560
    s.start_dot(MX, 172)
    A = s.node(MX, 200, "Resolving metadata,\nstandalone covers", "start")
    s.edge(f"M {MX} 178 L {MX} {A['top'] - 1}")

    # regions
    s.group(36, 300, 790, 1150, "For each codec: quality_priority, then aac-lc")
    s.group(270, 360, 545, 470, "Fetch phase, max_attempts tries")
    s.group(270, 900, 545, 530, "Decrypt phase, max_attempts tries")

    B = s.node(MX, 410, "Choosing a manifest", "check")
    s.edge(curve(MX, A['bottom'], MX, B['top'] - 1), "metadata resolved", (MX, 278))

    C1 = s.node(405, 520, "web enhancedHls\nfrom amp-api", "check", w=146)
    C2 = s.node(MX, 520, "device manifest\nwrapper /m3u8", "check", w=146)
    C3 = s.node(715, 520, "webplayback\nwrapper", "check", w=146)
    s.edge(curve(MX - 40, B['bottom'], C1['cx'], C1['top'] - 1), "other codecs", (455, 478))
    s.edge(curve(MX, B['bottom'], C2['cx'], C2['top'] - 1), "Hi-Res ALAC,\ncap >48 kHz\nor no cap", (MX, 478))
    s.edge(curve(MX + 40, B['bottom'], C3['cx'], C3['top'] - 1), "aac-lc", (665, 478))

    D = s.node(MX, 630, "Claiming the output path", "check")
    for c, x in ((C1, MX - 50), (C2, MX), (C3, MX + 50)):
        s.edge(curve(c['cx'], c['bottom'], x, D['top'] - 1))

    E = s.node(MX, 728, "Downloading the\nencrypted segments", "draw")
    s.edge(curve(MX, D['bottom'], MX, E['top'] - 1), "path is free:\ncover, lyrics once", (MX, 699))

    SK = s.node(925, 627, "skipped_existing", "stopped")
    s.edge(f"M {D['right']} {D['cy']} C 760 {D['cy']} 780 {SK['cy']} {SK['left'] - 1} {SK['cy']}",
           "file exists,\nno force_overwrite", (742, D['cy'] + 2))

    F = s.node(MX, 950, "Decrypting in-process\ninternal/fairplay", "draw")
    s.edge(curve(MX, E['bottom'], MX, F['top'] - 1), "encrypted bytes in\nRAM or temp_dir", (MX, 862))
    G = s.node(MX, 1052, "Remuxing with ffmpeg", "draw")
    s.edge(curve(MX, F['bottom'], MX, G['top'] - 1), "samples piped to\nffmpeg's stdin", (MX, 1019))
    H = s.node(MX, 1144, "Verifying with ffmpeg", "publish")
    s.edge(curve(MX, G['bottom'], MX, H['top'] - 1), "check_integrity:\ntrue", (MX, 1116))
    I = s.node(MX, 1232, "Tagging: metadata,\ncover, lyrics", "publish")
    s.edge(curve(MX, H['bottom'], MX, I['top'] - 1), "file is intact", (MX, 1206))
    s.edge(f"M {G['left']} {G['cy']} C 390 {G['cy']} 380 {G['cy'] + 30} 380 {G['cy'] + 80} "
           f"L 380 {I['cy'] - 40} C 380 {I['cy'] - 10} 400 {I['cy']} {I['left'] - 1} {I['cy']}",
           "check_integrity:\nfalse", (380, 1170))
    J = s.node(MX, 1336, "Moving the file into\ndownloads_dir", "publish")
    s.edge(curve(MX, I['bottom'], MX, J['top'] - 1), "tagged in temp_dir", (MX, 1306))

    DONE = s.node(MX, 1500, "completed", "wrote")
    s.edge(curve(MX, J['bottom'], MX, DONE['top'] - 1), "saved", (MX, 1474))

    # fallback, on the left
    K = s.node(150, 820, "Falling back to\nthe next codec", "check")
    s.edge(f"M 270 620 C 200 620 190 660 190 700 L 190 {K['top'] - 1}",
           "attempts run out,\nor codec not listed", (190, 740))
    s.edge(f"M 270 1160 C 210 1160 190 1120 190 1080 L 190 {K['bottom'] + 1}",
           "decrypt attempts\nrun out", (190, 1010))
    s.edge(f"M 110 {K['top']} L 110 470 C 110 440 130 428 160 428 L {B['left'] - 1} 428",
           "another codec\nis left", (175, 428))

    FAIL = s.node(110, 1500, "failed", "fail")
    s.edge(f"M 110 {K['bottom']} L 110 {FAIL['top'] - 1}", "no codec left", (110, 1290))
    s.edge(f"M {A['left']} {A['cy']} L 40 {A['cy']} C 22 {A['cy']} 18 {A['cy'] + 10} 18 {A['cy'] + 30} "
           f"L 18 {FAIL['cy'] - 30} C 18 {FAIL['cy'] - 8} 30 {FAIL['cy']} {FAIL['left'] - 1} {FAIL['cy']}",
           "metadata attempts\nrun out", (300, A['cy']))

    CAN = s.node(910, 1500, "cancelled", "stopped")
    s.edge(f"M 826 1380 C 900 1380 910 1420 910 {CAN['top'] - 1}", "job cancelled", (905, 1432))
    return s.render()


# ---------------------------------------------------------------------------
# Architecture
# ---------------------------------------------------------------------------
def architecture():
    s = SVG(1040, 1045,
            "amdl architecture",
            "The web console calls amdl's /api/v1 REST and SSE API. Inside amdl, the HTTP API feeds the "
            "job queue, which stores jobs, items and events in SQLite, fires job hooks, and runs each "
            "track through the media pipeline; the pipeline decrypts samples in-process with "
            "internal/fairplay and writes tagged files to downloads_dir. A wrapper, wrapper-manager v2 "
            "(recommended, console login) in front of wrapper-lite or a bare wrapper-lite, supplies "
            "white-box key templates, the device manifest for Hi-Res ALAC, lyrics and the AAC-LC "
            "license. Apple's amp-api serves the catalog with a token scraped from music.apple.com, and "
            "Apple's media CDN serves the encrypted segments.")
    s.heading = "amdl and what it talks to"
    s.legend(30, 60, 980, [("client", "start"), ("amdl", "check"), ("on disk", "wrote"),
                           ("wrapper", "draw"), ("Apple", "publish"), ("alternative", "stopped")])

    CON = s.node(200, 180, "Web console\nbrowser, /console/", "start")
    HOOK_T = s.node(860, 180, "Your webhook\nor command", "start")

    s.group(40, 280, 960, 400, "amdl: one Go process and its files", anchor="middle")
    API = s.node(200, 340, "HTTP API\ninternal/api", "check", w=180)
    JOBS = s.node(530, 340, "Job queue, workers\ninternal/jobs", "check", w=200)
    HOOKS = s.node(860, 340, "Job hooks\ninternal/hooks", "check", w=180)
    DD = s.node(200, 460, "downloads_dir\ntagged .m4a files", "wrote", w=180)
    MED = s.node(530, 460, "Track pipeline\ninternal/media", "check", w=200)
    DB = s.node(880, 460, "SQLite\njobs, items, events", "wrote", w=170)
    FP = s.node(600, 584, "FairPlay decrypt\ninternal/fairplay", "check", w=170)

    s.edge(curve(200, CON['bottom'], 200, API['top'] - 1), "/api/v1\nREST + SSE", (200, 254))
    s.edge(f"M {API['right']} {API['cy']} L {JOBS['left'] - 1} {JOBS['cy']}", "submit, cancel", (360, API['cy']))
    s.edge(f"M {JOBS['right']} {JOBS['cy']} L {HOOKS['left'] - 1} {HOOKS['cy']}", "job queued\nor ended", (700, JOBS['cy']))
    s.edge(f"M 860 {HOOKS['top']} L 860 {HOOK_T['bottom'] + 1}", "webhook or exec", (860, 254))
    s.edge(curve(530, JOBS['bottom'], 530, MED['top'] - 1), "run each track", (530, 424))
    s.edge(curve(600, JOBS['bottom'], 840, DB['top'] - 1), "store rows\nand events", (725, 424))
    s.edge(f"M {MED['left']} {MED['cy']} L {DD['right'] + 1} {DD['cy']}", "ffmpeg remux,\ntag, move", (360, MED['cy']))
    s.edge(curve(585, MED['bottom'], 600, FP['top'] - 1), "encrypted samples,\nkey template", (572, 546))

    s.group(40, 770, 440, 245, "Wrapper: Linux, user namespaces", anchor="end")
    s.group(520, 770, 480, 245, "Apple")
    WM = s.node(155, 830, "wrapper-manager v2\nrecommended", "draw")
    ALT = s.node(365, 830, "bare wrapper-lite\nlog in: --login", "stopped")
    WL = s.node(155, 950, "wrapper-lite\naccount session", "draw")
    s.edge(curve(155, WM['bottom'], 155, WL['top'] - 1), "one per account", (155, 912))
    AMP = s.node(670, 830, "amp-api\n.music.apple.com", "publish")
    CDN = s.node(880, 830, "media CDN\nHLS segments", "publish")

    # wrapper -> pipeline, login -> wrapper-manager
    s.edge(f"M 300 770 C 300 700 470 690 470 640 L 470 {MED['bottom'] + 1}",
           "/key: white-box key template\n/m3u8: device manifest\n(Hi-Res ALAC only)\n"
           "/lyrics, /webplayback, /license", (290, 722))
    s.edge(f"M {API['left']} {API['cy']} C 70 {API['cy']} 62 {API['cy'] + 12} 62 {API['cy'] + 40} "
           f"L 62 790 C 62 810 80 816 95 {WM['top'] - 1}",
           "console login\nPOST /login", (62, 722))
    s.edge(f"M 670 {AMP['top']} C 670 740 705 720 705 690 L 705 575 C 705 540 620 545 620 {MED['bottom'] + 1}",
           "catalog: metadata, enhancedHls\ntoken scraped from\nmusic.apple.com", (690, 722))
    s.edge(f"M 880 {CDN['top']} C 880 560 760 {MED['cy']} {MED['right'] + 1} {MED['cy']}",
           "encrypted\nsegments", (892, 722))
    return s.render()


if __name__ == "__main__":
    out = sys.argv[1]
    with open(f"{out}/architecture.svg", "w") as f:
        f.write(architecture())
    with open(f"{out}/track-download.svg", "w") as f:
        f.write(track())
