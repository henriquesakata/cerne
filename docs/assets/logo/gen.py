#!/usr/bin/env python3
"""Draws cerne's mark: the cross-section of a tree trunk, in one ink.

Every ring is the trunk's contour shrunk towards the pith, the point the tree
grew from. The pith sits off centre, so the rings end up wider apart on one
side, as in a trunk that grew unevenly. The innermost shape is filled: it is
the heartwood ("cerne").

Run it from anywhere:

    python3 docs/assets/logo/gen.py

It rewrites the two marks next to this file (one ink for a light page, one for
a dark page) and ui/public/favicon.svg, the reduced mark that switches ink with
the colour scheme. After changing the favicon, rebuild ui/dist.
"""
import math
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
LOGO = Path(__file__).resolve().parent

# Where the rings grew from, off the centre of the trunk. The reduced mark keeps
# its pith nearer the middle: at 16 px there is no room for a wide swing.
PITH = (59.0, 57.0)
REDUCED_PITH = (56.0, 54.6)
ANCHORS = 22  # points per ring
LIGHT_PAGE_INK = "#1f2328"
DARK_PAGE_INK = "#e6edf3"

# (scale towards the pith, stroke width); the last entry of each list is the
# heartwood, which is filled instead of stroked.
FULL = [(1.0, 5.0), (0.76, 3.2), (0.55, 2.6), (0.31, None)]
# What still reads at 16 px: two rings and the heartwood, every stroke at least
# 1 px wide and every gap at least 1.15 px. One ring and a dot read as a circle
# with a point in it, not as a trunk (B-0033).
REDUCED = [(1.0, 8.5), (0.55, 6.5), (0.23, None)]


def trunk(theta):
    """The trunk's contour: a circle bent by a few slow waves."""
    r = 41 * (
        1
        + 0.045 * math.cos(theta - 0.6)
        + 0.030 * math.cos(2 * theta + 1.1)
        + 0.018 * math.cos(3 * theta + 2.3)
        + 0.010 * math.cos(5 * theta + 0.4)
    )
    return 50 + r * math.cos(theta), 50 + r * math.sin(theta)


def ring(scale, index, pith, ripple):
    """The trunk's contour shrunk towards the pith, with a ripple of its own.

    ripple scales that ripple: 1 for the full mark, 0 for none.
    """
    points = []
    for i in range(ANCHORS):
        theta = 2 * math.pi * i / ANCHORS
        tx, ty = trunk(theta)
        x = pith[0] + scale * (tx - pith[0])
        y = pith[1] + scale * (ty - pith[1])
        wave = ripple * scale * (0.55 * math.cos(4 * theta + index * 1.7) + 0.35 * math.cos(7 * theta + index * 0.9))
        dx, dy = x - pith[0], y - pith[1]
        length = math.hypot(dx, dy) or 1
        points.append((x + wave * dx / length, y + wave * dy / length))
    return points


def num(value):
    return ("%.1f" % value).rstrip("0").rstrip(".")


def path_data(points):
    """A closed Catmull-Rom spline through the points, as cubic Béziers."""
    n = len(points)
    d = "M%s %s" % (num(points[0][0]), num(points[0][1]))
    for i in range(n):
        p0, p1, p2, p3 = points[(i - 1) % n], points[i], points[(i + 1) % n], points[(i + 2) % n]
        c1 = (p1[0] + (p2[0] - p0[0]) / 6, p1[1] + (p2[1] - p0[1]) / 6)
        c2 = (p2[0] - (p3[0] - p1[0]) / 6, p2[1] - (p3[1] - p1[1]) / 6)
        d += "C%s %s %s %s %s %s" % tuple(num(v) for v in (*c1, *c2, *p2))
    return d + "Z"


def shapes(spec, pith, ripple):
    rings = [(ring(scale, index, pith, ripple), width) for index, (scale, width) in enumerate(spec)]
    # The pith is off centre; the drawing as a whole sits in the middle of the box.
    xs = [x for points, _ in rings for x, _ in points]
    ys = [y for points, _ in rings for _, y in points]
    dx, dy = 50 - (min(xs) + max(xs)) / 2, 50 - (min(ys) + max(ys)) / 2
    return [(path_data([(x + dx, y + dy) for x, y in points]), width) for points, width in rings]


def mark(ink):
    body = ""
    for d, width in shapes(FULL, PITH, 1):
        if width is None:
            body += '<path fill="%s" d="%s"/>' % (ink, d)
        else:
            body += '<path fill="none" stroke="%s" stroke-width="%s" d="%s"/>' % (ink, num(width), d)
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">%s</svg>\n' % body


def favicon():
    style = (
        "<style>.r{fill:none;stroke:%s}.h{fill:%s}"
        "@media (prefers-color-scheme:dark){.r{stroke:%s}.h{fill:%s}}</style>"
    ) % (LIGHT_PAGE_INK, LIGHT_PAGE_INK, DARK_PAGE_INK, DARK_PAGE_INK)
    body = ""
    # No ripple here: at 16 px it cannot be seen, and it narrows the gaps.
    for d, width in shapes(REDUCED, REDUCED_PITH, 0):
        if width is None:
            body += '<path class="h" d="%s"/>' % d
        else:
            body += '<path class="r" stroke-width="%s" d="%s"/>' % (num(width), d)
    return '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">%s%s</svg>\n' % (style, body)


if __name__ == "__main__":
    (LOGO / "cerne-mark-light.svg").write_text(mark(LIGHT_PAGE_INK))
    (LOGO / "cerne-mark-dark.svg").write_text(mark(DARK_PAGE_INK))
    public = ROOT / "ui" / "public"
    public.mkdir(exist_ok=True)
    (public / "favicon.svg").write_text(favicon())
