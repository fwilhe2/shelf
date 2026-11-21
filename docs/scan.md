# Scanning with the phone

shelf's web UI can scan paper documents into a PDF with a phone's camera. It
needs no app: open shelf in the phone's browser, go to a folder, and press
**Scan**.

1. **Take a picture of the first page.** The phone's own camera app opens, with
   its focus, flash and zoom. Lay the page flat in good light and fill the
   picture with it.
2. **Next page**, for as many pages as the document has. *Choose pictures*
   adds pictures already taken instead.
3. Fix the order (‹ ›), turn a page (↻), or remove one.
4. Choose the look: **Document** (gray, the paper made white, best for letters
   and for reading the text), **Grayscale**, or **Color** (photos, colourful
   forms). The choice is remembered on this device.
5. Name it (it starts as `Scan 2026-11-21 14-03`) and press **Save PDF**.

The PDF is saved in the folder you were in, like any upload, so an
interrupted upload leaves nothing half-written. With search on, the server
indexes it half a minute later. OCR then reads its text, and a minute or two
later you can find the letter by any word in it, such as a customer number.

## How it works

- The camera opens through an ordinary file input with
  `capture="environment"`, which every phone browser supports (Chrome,
  Firefox, Safari, Samsung Internet). This also works over plain HTTP; a live
  camera view inside the page (`getUserMedia`) would need HTTPS.
- Everything else happens in the browser: each picture is turned upright (by
  its EXIF orientation), scaled to 2480 pixels on its long side (A4 at about
  210 dpi, plenty for OCR), and for *Document* made gray, with its levels
  stretched so that the brighter half (the paper) becomes white and the
  darkest 2 % black. Each page is then a JPEG.
- `makePDF` in `web/index.html` writes the PDF itself: one page per picture,
  A4 wide and as high as the picture, the JPEG stored as it is (`DCTDecode`),
  so it is not compressed twice. A page is typically 200–500 KB. No library is
  involved.
- The pictures stay on the phone until *Save PDF*. Leaving the scan with
  pages in it (Back, ←, Esc) asks first.

## What it does not do

It does not find the edges of the page or straighten a page photographed at an
angle, as dedicated scanner apps do. Hold the phone straight above the page.
If you have such an app (the iOS Notes / Files *Scan Documents*, Google
Drive's scanner), let it make the PDF and upload that with *Upload*. shelf
indexes it the same way.
