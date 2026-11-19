#!/bin/bash
# Fills a directory with demo files to try full-text search on: PDFs with and
# without a text layer, scans, photos with text in them, office documents,
# plain text. Every file is either in the public domain by law (works of the
# US federal government, official German works under § 5 UrhG) or made here
# from public domain texts and invented content. SOURCES.md in the directory
# lists where each file comes from.
#
#   test/demo-data.sh ~/nas-data/demo
#
# Needs curl, ImageMagick (magick) and LibreOffice (soffice).
set -euo pipefail

out=${1:?usage: $0 DIRECTORY}
mkdir -p "$out"
out=$(cd "$out" && pwd)
mkdir -p "$out"/{pdf,scans,photos,office,text}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

ua='shelf-demo-data/1.0 (test fixture fetch)'
sources=$out/SOURCES.md
cat >"$sources" <<'EOF'
# Demo data: where each file comes from

| File | Source | Why it is free to use |
|---|---|---|
EOF
src() { printf '| `%s` | %s | %s |\n' "$1" "$2" "$3" >>"$sources"; }

# fetch FILE URL TYPE: downloads URL to FILE and checks `file` says TYPE.
fetch() {
	echo "fetch $1"
	curl -fsSL -A "$ua" -o "$out/$1" "$2"
	file -b "$out/$1" | grep -q "$3" || { echo "$1: not $3" >&2; exit 1; }
}

usgov='Work of the US federal government, public domain (17 U.S.C. § 105)'

# --- Downloaded ---------------------------------------------------------------

fetch pdf/irs-form-w4.pdf https://www.irs.gov/pub/irs-pdf/fw4.pdf PDF
src pdf/irs-form-w4.pdf "IRS Form W-4, https://www.irs.gov/pub/irs-pdf/fw4.pdf" "$usgov"

fetch pdf/grundgesetz.pdf https://www.gesetze-im-internet.de/gg/GG.pdf PDF
src pdf/grundgesetz.pdf "Grundgesetz, https://www.gesetze-im-internet.de/gg/GG.pdf" \
	'Official German work (Gesetz), not protected by copyright (§ 5 Abs. 1 UrhG)'

fetch photos/apollo-11-plaque.jpg https://images-assets.nasa.gov/image/S69-39334/S69-39334~large.jpg JPEG
src photos/apollo-11-plaque.jpg "NASA photo S69-39334, https://images.nasa.gov/details/S69-39334" \
	"$usgov; NASA media guidelines"

fetch photos/kennedy-hq-sign.jpg https://images-assets.nasa.gov/image/KSC-20210218-PH-GEB03_0026/KSC-20210218-PH-GEB03_0026~large.jpg JPEG
src photos/kennedy-hq-sign.jpg "NASA photo KSC-20210218-PH-GEB03_0026, https://images.nasa.gov/details/KSC-20210218-PH-GEB03_0026" \
	"$usgov; NASA media guidelines. Its sign is too small for OCR: a photo found only by name"

fetch scans/zimmermann-telegram-1917.jpg "https://upload.wikimedia.org/wikipedia/commons/f/f0/Zimmermann_Telegram_as_Received_by_the_German_Ambassador_to_Mexico_-_NARA_-_302025.jpg" JPEG
src scans/zimmermann-telegram-1917.jpg "National Archives (NARA) 302025, via https://commons.wikimedia.org/wiki/File:Zimmermann_Telegram_as_Received_by_the_German_Ambassador_to_Mexico_-_NARA_-_302025.jpg" \
	"Published 1917, public domain; scan by NARA, $usgov"

# --- Made here ----------------------------------------------------------------

made='Made by test/demo-data.sh'
pd_text='text in the public domain (author died more than 70 years ago)'
invented='invented content, CC0'

cat >"$work/gettysburg.txt" <<'EOF'
The Gettysburg Address
Abraham Lincoln, November 19, 1863

Four score and seven years ago our fathers brought forth on this continent, a new nation, conceived in Liberty, and dedicated to the proposition that all men are created equal.

Now we are engaged in a great civil war, testing whether that nation, or any nation so conceived and so dedicated, can long endure. We are met on a great battle-field of that war. We have come to dedicate a portion of that field, as a final resting place for those who here gave their lives that that nation might live. It is altogether fitting and proper that we should do this.

But, in a larger sense, we can not dedicate -- we can not consecrate -- we can not hallow -- this ground. The brave men, living and dead, who struggled here, have consecrated it, far above our poor power to add or detract. The world will little note, nor long remember what we say here, but it can never forget what they did here. It is for us the living, rather, to be dedicated here to the unfinished work which they who fought here have thus far so nobly advanced. It is rather for us to be here dedicated to the great task remaining before us -- that from these honored dead we take increased devotion to that cause for which they gave the last full measure of devotion -- that we here highly resolve that these dead shall not have died in vain -- that this nation, under God, shall have a new birth of freedom -- and that government of the people, by the people, for the people, shall not perish from the earth.
EOF

cat >"$work/froschkoenig.txt" <<'EOF'
Der Froschkönig oder der eiserne Heinrich
Brüder Grimm, Kinder- und Hausmärchen

In den alten Zeiten, wo das Wünschen noch geholfen hat, lebte ein König, dessen Töchter waren alle schön, aber die jüngste war so schön, daß die Sonne selber, die doch so vieles gesehen hat, sich verwunderte, sooft sie ihr ins Gesicht schien. Nahe bei dem Schlosse des Königs lag ein großer dunkler Wald, und in dem Walde unter einer alten Linde war ein Brunnen; wenn nun der Tag recht heiß war, so ging das Königskind hinaus in den Wald und setzte sich an den Rand des kühlen Brunnens; und wenn sie Langeweile hatte, so nahm sie eine goldene Kugel, warf sie in die Höhe und fing sie wieder; und das war ihr liebstes Spielwerk.

Nun trug es sich einmal zu, daß die goldene Kugel der Königstochter nicht in ihr Händchen fiel, das sie in die Höhe gehalten hatte, sondern vorbei auf die Erde schlug und geradezu ins Wasser hineinrollte. Die Königstochter folgte ihr mit den Augen nach, aber die Kugel verschwand, und der Brunnen war tief, so tief, daß man keinen Grund sah.
EOF

cat >"$work/invoice.txt" <<'EOF'
Tischlerei Holzwurm GmbH
Sägewerkstraße 12, 99999 Musterstadt

Rechnung Nr. 2026-0417
Kundennummer: KD-58213
Datum: 14. März 2026

Pos.  Leistung                         Betrag
1     Bücherregal Eiche, maßgefertigt  1.240,00 EUR
2     Lieferung und Montage              180,00 EUR

Summe netto                            1.420,00 EUR
USt. 19 %                                269,80 EUR
Gesamtbetrag                           1.689,80 EUR

Zahlbar bis 28. März 2026 ohne Abzug.
EOF

echo "make text"
cp "$work/gettysburg.txt" "$out/text/gettysburg-address.txt"
src text/gettysburg-address.txt "$made" "Abraham Lincoln, 1863: $pd_text"
{
	echo '# Einkaufsliste'
	echo
	printf -- '- %s\n' Haferflocken Äpfel 'Kaffee (Bohnen)' Spülmittel Glühbirnen
} >"$out/text/einkaufsliste.md"
src text/einkaufsliste.md "$made" "$invented"
cat >"$out/text/stromzaehler.csv" <<'EOF'
Datum,Zählerstand kWh,Bemerkung
2026-01-01,48213,Ablesung Neujahr
2026-04-01,49020,
2026-07-01,49611,Urlaub im Juli
2026-10-01,50188,neuer Kühlschrank
EOF
src text/stromzaehler.csv "$made" "$invented"

# Office documents: HTML through LibreOffice, so every format holds the same kind of text.
html() { # TITLE TEXTFILE
	{
		printf '<html><head><meta charset="utf-8"><title>%s</title></head><body>\n' "$1"
		awk 'NR==1{print "<h1>"$0"</h1>";next} NF{print "<p>"$0"</p>"}' "$2"
		echo '</body></html>'
	} >"$work/$3.html"
}
office() { # FILE FILTER INPUT
	soffice -env:UserInstallation=file://"$work/lo" --headless --convert-to "$2" --outdir "$work/conv" "$3" >/dev/null 2>&1
	mv "$work/conv/$(basename "${3%.*}").${1##*.}" "$out/$1"
}
echo "make office documents (LibreOffice)"
html 'Der Froschkönig' "$work/froschkoenig.txt" froschkoenig
html 'Gettysburg Address' "$work/gettysburg.txt" gettysburg
html 'Rechnung' "$work/invoice.txt" rechnung
office office/froschkoenig.odt 'odt:writer8' "$work/froschkoenig.html"
src office/froschkoenig.odt "$made" "Brothers Grimm, 1857 edition: $pd_text"
office office/gettysburg-address.docx 'docx:MS Word 2007 XML' "$work/gettysburg.html"
src office/gettysburg-address.docx "$made" "Abraham Lincoln, 1863: $pd_text"
cp "$work/rechnung.html" "$work/Rechnung Holzwurm März 2026.html"
office "pdf/Rechnung Holzwurm März 2026.pdf" 'pdf:writer_pdf_Export' "$work/Rechnung Holzwurm März 2026.html"
src "pdf/Rechnung Holzwurm März 2026.pdf" "$made" "$invented; a PDF with a text layer"

cat >"$work/haushaltsbuch.csv" <<'EOF'
Monat,Miete,Strom,Lebensmittel,Versicherung,Summe
Januar,950,72,410,38,=SUM(B2:E2)
Februar,950,72,385,38,=SUM(B3:E3)
März,950,72,432,38,=SUM(B4:E4)
EOF
office office/haushaltsbuch.ods 'ods:calc8' "$work/haushaltsbuch.csv"
src office/haushaltsbuch.ods "$made" "$invented"
office office/haushaltsbuch.xlsx 'xlsx:Calc MS Excel 2007 XML' "$work/haushaltsbuch.csv"
src office/haushaltsbuch.xlsx "$made" "$invented"

# Pictures of text, and PDFs that are only pictures: what OCR is for.
font=$(fc-match -f '%{file}' 'DejaVu Sans' 2>/dev/null || true)
render() { # TEXTFILE OUTPUT.png
	magick -size 1654x -background white -fill black ${font:+-font "$font"} -pointsize 30 \
		-interline-spacing 8 caption:@"$1" -bordercolor white -border 120 "$2"
}
echo "make scans and photos (ImageMagick)"
render "$work/invoice.txt" "$work/invoice.png"
# A phone photo of a printout: tilted, on a grey table, a little blurred and noisy.
magick "$work/invoice.png" -resize 60% -background '#8a8580' -rotate 4 \
	-bordercolor '#8a8580' -border 80 -blur 0x0.8 -attenuate 0.4 +noise Gaussian \
	-quality 85 "$out/photos/rechnung-handyfoto.jpg"
src photos/rechnung-handyfoto.jpg "$made" "$invented; text only in the picture"

# A scanned letter: a PDF holding nothing but a page image.
render "$work/froschkoenig.txt" "$work/frosch.png"
magick "$work/frosch.png" -colorspace Gray -rotate 0.7 -attenuate 0.2 +noise Gaussian \
	-units PixelsPerInch -density 200 "$out/scans/froschkoenig-gescannt.pdf"
src scans/froschkoenig-gescannt.pdf "$made" "Brothers Grimm: $pd_text; a PDF without a text layer"

render "$work/gettysburg.txt" "$work/getty.png"
magick "$work/getty.png" -colorspace Gray -units PixelsPerInch -density 200 "$out/scans/gettysburg-scan.tiff"
src scans/gettysburg-scan.tiff "$made" "Abraham Lincoln, 1863: $pd_text"

magick "$work/invoice.png" -colorspace Gray -units PixelsPerInch -density 200 "$out/scans/rechnung-gescannt.pdf"
src scans/rechnung-gescannt.pdf "$made" "$invented; a PDF without a text layer"

echo
echo "Demo data in $out:"
(cd "$out" && find . -type f ! -name SOURCES.md | sort)
