# fonts

GNU Unifont 18.0.01, converted to WOFF2, so that the demo page can draw the
characters it names.

The terminal in the page is a character table as well as a dictionary, and a
character table that renders half its entries as empty boxes is not one. No
system font covers Unicode; Unifont is the only font that comes close, being
a 16-pixel bitmap font with a glyph for very nearly every assigned code point.
Being a terminal-shaped bitmap font is also why it suits a terminal: the
glyphs are one or two cells wide by construction.

    unifont.woff2         892 KB   plane 0, the Basic Multilingual Plane
    unifont_upper.woff2   593 KB   planes 1 to 15: emoji, historic scripts

They are split because the page declares each with a `unicode-range`, so the
second is fetched only when something outside plane 0 is actually drawn.

Rebuilt with:

    curl -O https://unifoundry.com/pub/unifont/unifont-18.0.01/font-builds/unifont-18.0.01.otf
    curl -O https://unifoundry.com/pub/unifont/unifont-18.0.01/font-builds/unifont_upper-18.0.01.otf
    woff2_compress unifont-18.0.01.otf
    woff2_compress unifont_upper-18.0.01.otf

Unifont is dual-licensed under the SIL Open Font License 1.1 and the GNU GPL
version 2 or later with the font embedding exception. See LICENSE-unifont.
