# Browser tests

Drive shelf's web UI in Chromium and Firefox with
[Playwright](https://playwright.dev): the file viewer (pictures, video,
music, PDFs, text), office documents in grind's page, and search (with a
stand-in for clerk that `server.sh` writes).

```sh
cd test/browser
npm ci
npx playwright install chromium firefox     # once per Playwright version
GRIND_WEB=~/code/github.com/fwilhe2/grind/ui_web/dist npx playwright test
npx playwright test --project firefox       # one browser
npx playwright test --headed --debug        # watch it
```

`playwright.config.js` starts `server.sh`, which builds shelf, writes the test
files with `fixtures.js` into a temporary share and serves it on
`127.0.0.1:18090` (`PORT=` to change it), user `alice`, password `pw`.

Firefox plays audio only with an audio server (PipeWire or PulseAudio)
running; without one, "music plays" fails with `MEDIA_ERR_DECODE`. CI starts
PulseAudio, which gives it a silent sink.

`GRIND_WEB` is grind's built web page. Without it, the office tests are
skipped; in CI it is copied from `ghcr.io/fwilhe2/grind-web`, like the
container image does, and required.

Dependabot proposes new Playwright releases (two weeks after they come out),
which bring new Chromium and Firefox builds along.
