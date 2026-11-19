// Writes the files the browser tests open into a share directory:
//
//   node fixtures.js DIR
//
// Everything is generated here except clip.webm (2 s of ffmpeg's testsrc,
// VP9, which both Chromium and Firefox builds of Playwright can play).
"use strict";
const fs = require("fs");
const path = require("path");
const zlib = require("zlib");

const dir = process.argv[2];
if (!dir) throw new Error("usage: node fixtures.js DIR");
fs.mkdirSync(path.join(dir, "sub"), { recursive: true });
const write = (name, data) => fs.writeFileSync(path.join(dir, name), data);

// ---- PNG: an RGB gradient, so a broken decode is visible on a screenshot
function crc32(buf) {
  let c, crc = 0xffffffff;
  for (const b of buf) {
    c = (crc ^ b) & 0xff;
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    crc = (crc >>> 8) ^ c;
  }
  return (crc ^ 0xffffffff) >>> 0;
}
function chunk(type, data) {
  const len = Buffer.alloc(4), crc = Buffer.alloc(4), td = Buffer.concat([Buffer.from(type), data]);
  len.writeUInt32BE(data.length);
  crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
}
function png(w, h, pixel) {
  const raw = Buffer.alloc((w * 3 + 1) * h);
  for (let y = 0; y < h; y++) {
    raw[y * (w * 3 + 1)] = 0;
    for (let x = 0; x < w; x++) raw.set(pixel(x, y), y * (w * 3 + 1) + 1 + x * 3);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr.set([8, 2, 0, 0, 0], 8); // 8 bit, RGB
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", ihdr),
    chunk("IDAT", zlib.deflateSync(raw)), chunk("IEND", Buffer.alloc(0))]);
}
write("a-photo.png", png(640, 400, (x, y) => [x * 255 / 640, y * 255 / 400, 160]));
write("b-photo.png", png(300, 500, (x, y) => [40, x * 255 / 300, y * 255 / 500]));
// Bigger than the viewer in both directions: it must be scaled down to fit.
write("large-photo.png", png(1600, 1300, (x, y) => [200, x * 255 / 1600, y * 255 / 1300]));

// ---- PDF: two pages of text in Helvetica, which PDFs may use without embedding it
function pdf(pages) {
  const objs = [];
  const add = s => objs.push(s) && objs.length;
  const catalog = add(""), pageTree = add(""), font = add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>");
  const kids = pages.map(text => {
    const stream = `BT /F1 28 Tf 72 700 Td (${text}) Tj ET`;
    const content = add(`<< /Length ${stream.length} >>\nstream\n${stream}\nendstream`);
    return add(`<< /Type /Page /Parent ${pageTree} 0 R /MediaBox [0 0 612 792] /Contents ${content} 0 R /Resources << /Font << /F1 ${font} 0 R >> >> >>`);
  });
  objs[catalog - 1] = `<< /Type /Catalog /Pages ${pageTree} 0 R >>`;
  objs[pageTree - 1] = `<< /Type /Pages /Kids [${kids.map(k => k + " 0 R").join(" ")}] /Count ${kids.length} >>`;
  let out = "%PDF-1.4\n";
  const offsets = objs.map((o, i) => { const at = out.length; out += `${i + 1} 0 obj\n${o}\nendobj\n`; return at; });
  const xref = out.length;
  out += `xref\n0 ${objs.length + 1}\n0000000000 65535 f \n` + offsets.map(o => String(o).padStart(10, "0") + " 00000 n \n").join("");
  out += `trailer\n<< /Size ${objs.length + 1} /Root ${catalog} 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  return out;
}
write("testpage.pdf", pdf(["Hello from shelf", "Second page"]));

// ---- WAV: one second of a 440 Hz tone
{
  const rate = 8000, n = rate, b = Buffer.alloc(44 + n * 2);
  b.write("RIFF", 0); b.writeUInt32LE(36 + n * 2, 4); b.write("WAVEfmt ", 8);
  b.writeUInt32LE(16, 16); b.writeUInt16LE(1, 20); b.writeUInt16LE(1, 22);
  b.writeUInt32LE(rate, 24); b.writeUInt32LE(rate * 2, 28); b.writeUInt16LE(2, 32); b.writeUInt16LE(16, 34);
  b.write("data", 36); b.writeUInt32LE(n * 2, 40);
  for (let i = 0; i < n; i++) b.writeInt16LE(Math.round(8000 * Math.sin(2 * Math.PI * 440 * i / rate)), 44 + i * 2);
  write("ding.wav", b);
}

// ---- office documents for grind: a flat ODF spreadsheet (a space in the name on purpose)
// and a file that only claims to be one
write("My budget.fods", `<?xml version="1.0" encoding="UTF-8"?>
<office:document xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
  xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"
  xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
  office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.spreadsheet">
 <office:body><office:spreadsheet><table:table table:name="Budget">
  <table:table-row>
   <table:table-cell office:value-type="string"><text:p>Groceries</text:p></table:table-cell>
   <table:table-cell office:value-type="float" office:value="500"><text:p>500</text:p></table:table-cell>
  </table:table-row>
 </table:table></office:spreadsheet></office:body>
</office:document>
`);
write("broken.ods", "not really a spreadsheet\n");

fs.copyFileSync(path.join(__dirname, "fixtures", "clip.webm"), path.join(dir, "clip.webm"));
write("notes.md", "# Notes\n\nHello from shelf.\n\tA tabbed line.\n");
write("blob.bin", Buffer.from([0, 1, 2, 3]));
