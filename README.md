# shelf

A simple NAS for your home network. One binary, one config file, no database, no
enterprise features. Runs directly on the host or in a rootless Podman container.

- **Web UI**: browse, upload (including drag & drop of whole folders), download,
  download folders as zip, rename, delete, new folder. Works on phones too.
- **Office documents** (optional): open spreadsheets and text documents
  (`.ods`, `.odt`, `.xlsx`, `.docx`, …) right in the browser with
  [grind](https://github.com/fwilhe2/grind). See below.
- **Network drive**: every share can be mounted via WebDAV in Linux file managers,
  macOS Finder and Windows Explorer.
- **Users and shares**: a share is a directory on disk. Shares can be read-only
  and limited to some users.
- **Safe by default**: users can't leave a share's directory, not through `..`
  and not through symlinks. Passwords are stored as bcrypt hashes.

## Build

Requires Go 1.26 or newer.

```sh
go build -o shelf .

# for a Raspberry Pi 3/4/5 running a 64-bit OS:
GOOS=linux GOARCH=arm64 go build -o shelf .
```

## Quick start (just try it)

```sh
export SHELF_CONFIG=$PWD/shelf.json
./shelf init -data ~/nas-data      # asks for a user name and password
./shelf serve
```

Open the printed address (e.g. `http://192.168.1.20:8080/`) in a browser.

## Install as a service

```sh
sudo install -m 755 shelf /usr/local/bin/shelf
sudo useradd --system --home-dir /srv/shelf --shell /usr/sbin/nologin shelf
sudo mkdir -p /srv/shelf && sudo chown shelf:shelf /srv/shelf

sudo shelf init -data /srv/shelf
sudo chown shelf:shelf /etc/shelf/config.json

sudo install -m 644 shelf.service /etc/systemd/system/
sudo systemctl enable --now shelf
journalctl -u shelf            # shows the addresses it is reachable on
```

If you'd rather keep files owned by your own account, set `User=`/`Group=` in
`shelf.service` to your user and run `init` with `-data` pointing to a directory
you own. If share directories are under `/home`, remove `ProtectHome=read-only`.

## Run in a container (Podman, rootless, e.g. Fedora Silverblue)

The `Containerfile` builds a ~21 MB image: a static binary plus
[grind's web page](#office-documents) (~10 MB) on
`distroless/static:nonroot`, without a shell or package manager. The container runs
rootless via a [Quadlet](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
unit with a read-only root file system, no capabilities and `no-new-privileges`.

Config and files are **bind mounts** of plain host directories:
`~/.config/shelf` → `/config` and `~/nas-data` → `/data`. With
`UserNS=keep-id`, the container user is mapped to your own user, so the files on
the host belong to you and you can back them up, browse them or move them to
another disk like any other files. Edit the `Volume=` lines in
`deploy/shelf.container` to keep the data somewhere else, e.g. on a second disk
under `/var/mnt`.

```sh
podman build -t localhost/shelf:latest .
mkdir -p ~/.config/shelf ~/nas-data

# create the config and the first user
podman run --rm -it --userns keep-id:uid=65532,gid=65532 \
  -v ~/.config/shelf:/config:z -v ~/nas-data:/data:z \
  localhost/shelf init -data /data -listen :8443

# turn on HTTPS: list the names and IPs your devices use to reach the NAS
podman run --rm --userns keep-id:uid=65532,gid=65532 -v ~/.config/shelf:/config:z \
  localhost/shelf cert -hosts nas.local,192.168.1.20

# install and start the service
mkdir -p ~/.config/containers/systemd
cp deploy/shelf.container ~/.config/containers/systemd/
systemctl --user daemon-reload
systemctl --user start shelf
loginctl enable-linger $USER     # keep it running when you are logged out
journalctl --user -u shelf       # logs
```

Then open `https://<your-nas>:8443/` and trust the certificate (see [HTTPS](#https)).
Fedora's default firewall zone on Workstation/Silverblue already allows ports
above 1024. Elsewhere, run `sudo firewall-cmd --permanent --add-port=8443/tcp && sudo firewall-cmd --reload`.

Manage users and shares inside the running container (changes apply without restart):

```sh
alias shelf='podman exec -it shelf /shelf'
shelf user add mom
shelf share add photos /data/photos
```

Paths in the config are paths inside the container, so new shares go under
`/data`. To share another host directory, add a `Volume=` line to the unit first.

To update: pull the new source, run `podman build -t localhost/shelf:latest .`
again and then `podman auto-update` (or `systemctl --user restart shelf`).

### Automatic backups

`deploy/shelf-backup` takes a snapshot of `~/.config/shelf` and `~/nas-data` with
rsync once a day. Each snapshot is a normal directory with a full copy you can
browse. Files that didn't change are hard links to the previous snapshot, so
each snapshot only uses space for what changed. Restoring is just copying files
back.

```sh
install -D -m 755 deploy/shelf-backup ~/.local/bin/shelf-backup
cp deploy/shelf-backup.service deploy/shelf-backup.timer ~/.config/systemd/user/
echo 'BACKUP_DEST=/run/media/'$USER'/backupdisk/shelf' > ~/.config/shelf/backup.env
mkdir -p /run/media/$USER/backupdisk/shelf    # on the backup disk, once
systemctl --user daemon-reload
systemctl --user enable --now shelf-backup.timer
systemctl --user start shelf-backup           # first backup now
```

The destination must exist already. If it is missing (backup disk not mounted),
the backup fails instead of filling up the system disk. The last 14 snapshots
are kept (`BACKUP_KEEP=` in `backup.env`). The destination should be on a
different disk, ideally one you can also keep somewhere else.

## HTTPS

```sh
shelf cert                                   # on the host: names and IPs are detected
shelf cert -hosts nas.local,192.168.1.20     # or list them yourself (required in a container)
```

This creates a small private certificate authority (`ca.crt`, `ca.key` next to
the config file) and a server certificate signed by it, and turns on HTTPS in
the config. Restart shelf afterwards. No domain, no Let's Encrypt, no internet
access needed, and no reverse proxy: shelf does TLS itself.

**Each device has to trust the CA once.** Without that, browsers show a warning
and some clients (Windows Explorer in particular) refuse to connect at all.
Open `https://<your-nas>:8443/ca.crt`, accept the warning this one time, and
install the downloaded file:

| System | How |
|---|---|
| Fedora / Linux | `sudo trust anchor --store shelf-ca.crt` (Firefox: Settings → Certificates → Import) |
| Windows | double-click → *Install Certificate* → *Local Machine* → *Trusted Root Certification Authorities* |
| macOS | double-click → Keychain *System* → open the certificate → *Trust* → *Always Trust* |
| iOS / iPadOS | open the link in Safari → Settings → *Profile Downloaded* → install, then Settings → General → About → Certificate Trust Settings → turn it on |
| Android | Settings → Security → *Encryption & credentials* → *Install a certificate* → *CA certificate* |

After that, the connection is trusted without warnings. You only do this once
per device: shelf renews its server certificate automatically before it
expires, and running `shelf cert -hosts ...` again for new names or IPs keeps
the same CA.

The CA is name-constrained: it can only sign for private IP addresses,
`.local`, `.lan`, `.home.arpa`, `.internal` and the names given when it was
created. So even if `ca.key` leaks, it can't be used against other websites on
your devices. If you later need a name it doesn't cover, `shelf cert -new-ca`
creates a new CA, and every device has to trust the new one.

You can also use your own certificate: set `tls_cert` and `tls_key` in the
config.

## Managing users and shares

```sh
shelf user add mom                 # asks for a password
shelf user passwd mom
shelf user del mom
shelf user list

shelf share add photos /mnt/usb/photos                  # everyone, read-write
shelf share add movies /mnt/usb/movies -ro              # everyone, read-only
shelf share add taxes  /srv/shelf/taxes -users florian  # only florian
shelf share del photos                                  # files are kept
shelf share list
```

A running server picks up changes automatically, so you don't need to restart it.
Use `sudo` (or `-c FILE` / `SHELF_CONFIG`) when the config is in `/etc/shelf`.

## Office documents

shelf can open office documents in the browser with the web version of
[grind](https://github.com/fwilhe2/grind), an office suite that runs entirely in
the browser (WebAssembly). In the file list, the name of an `.ods`, `.odt`,
`.fods`, `.fodt`, `.xlsx` or `.docx` file then opens it in a new tab.

For now, documents are opened from the share but **not saved back**: you can view
and edit them, and *Save* downloads the edited copy as ODF. Saving back to the
share is planned, see [docs/office.md](docs/office.md).

The container image includes grind's page and turns this on. Without the
container, point shelf to a built grind page (`ui_web/dist` of grind, e.g. from
`podman create ghcr.io/fwilhe2/grind-web` and `podman cp …:/usr/share/grind/web`)
with `"office_dir"` in the config or `$SHELF_OFFICE_DIR`. Without either, it is off.

## Connecting as a network drive

The WebDAV address is `http://<your-nas>:8080/dav/` (with HTTPS:
`https://<your-nas>:8443/dav/`, and `davs://` instead of `dav://` in the Linux
file managers). Log in with your shelf user
name and password. The "Connect a drive" button in the web UI shows the exact
address.

| System | How |
|---|---|
| GNOME Files | *Other Locations* → *Connect to Server* → `dav://<your-nas>:8080/dav/` |
| KDE Dolphin | address bar: `webdav://<your-nas>:8080/dav/` |
| macOS | Finder → *Go* → *Connect to Server* (⌘K) → `http://<your-nas>:8080/dav/` |
| Windows | *This PC* → *Map network drive* → `http://<your-nas>:8080/dav/` |
| Linux, permanent mount | `davfs2` (`mount -t davfs http://<your-nas>:8080/dav/ /mnt/nas`) or `rclone mount` |
| Android / iOS | any file manager with WebDAV support |

**Windows over plain HTTP:** with HTTPS (and the CA trusted), nothing to do here. Windows refuses passwords over unencrypted HTTP
unless you allow it once. In an admin shell, run:

```
reg add HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters /v BasicAuthLevel /t REG_DWORD /d 2 /f
net stop webclient && net start webclient
```

Windows also limits WebDAV downloads to about 50 MB per file by default. You can
raise `FileSizeLimitInBytes` (same registry key, max `0xffffffff`), or use
[rclone](https://rclone.org) or the web UI for very large files.

## Configuration file

`/etc/shelf/config.json` (or `-c FILE`, or `$SHELF_CONFIG`):

```json
{
  "listen": ":8080",
  "users": [
    { "name": "florian", "password_hash": "$2a$10$..." }
  ],
  "shares": [
    { "name": "files",  "path": "/srv/shelf" },
    { "name": "movies", "path": "/mnt/usb/movies", "read_only": true },
    { "name": "taxes",  "path": "/srv/shelf-taxes", "users": ["florian"] }
  ]
}
```

To change `listen`, edit the file and restart the server. Optional:
`"tls_cert"` and `"tls_key"` (PEM file paths) turn on HTTPS, and `"tls_ca"` is
offered for download at `/ca.crt`. `shelf cert` sets all three. `"office_dir"`
turns on [office documents](#office-documents).

## Security notes

shelf is meant for a trusted home network. Traffic is unencrypted plain HTTP
unless you turn on HTTPS with `shelf cert`. Don't expose shelf directly to the internet. For
access from outside, use a VPN such as WireGuard or Tailscale.

## Layout of the code

| File | What |
|---|---|
| `main.go` | command line (`init`, `serve`, `user`, `share`) |
| `config.go` | config file loading, validation, atomic saving |
| `server.go` | HTTP: login, JSON API for the UI, zip download, WebDAV |
| `office.go` | serves grind's web page for office documents under `/office/` |
| `cert.go` | private CA, server certificates, automatic renewal |
| `fs.go` | the virtual file system: `/` lists shares, each share is an `os.Root` |
| `web/index.html` | the whole web UI, embedded into the binary |
| `Containerfile`, `deploy/` | container image, Quadlet unit, backup script and timer |
| `test/e2e.sh` | end-to-end test and demo of the container (run by CI, see below) |

## Tests

`go test ./...` runs the unit tests. `test/e2e.sh` builds nothing itself: after
`podman build -t localhost/shelf:latest .` it runs the image like
`deploy/shelf.container`, sets it up with HTTPS, two users and three shares, uploads,
downloads, renames and deletes files over the web API and WebDAV, and checks on
the host that every file was stored correctly, that access rules hold, that
nothing is lost on a restart and that the backup script works. CI runs both on
every push; the end-to-end run's report is in the job summary.

## License

shelf is licensed under the GNU Affero General Public License v3.0 or later, see
[LICENSE](LICENSE).
