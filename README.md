# mynas

A simple NAS for your home network. One binary, one config file, no database, no
containers, no enterprise features.

- **Web UI**: browse, upload (including drag & drop of whole folders), download,
  download folders as zip, rename, delete, new folder. Works on phones too.
- **Network drive**: every share can be mounted via WebDAV in Linux file managers,
  macOS Finder and Windows Explorer.
- **Users and shares**: a share is a directory on disk. Shares can be read-only
  and limited to some users.
- **Safe by default**: users can't leave a share's directory, not through `..`
  and not through symlinks. Passwords are stored as bcrypt hashes.

## Build

Requires Go 1.26 or newer.

```sh
go build -o mynas .

# for a Raspberry Pi 3/4/5 running a 64-bit OS:
GOOS=linux GOARCH=arm64 go build -o mynas .
```

## Quick start (just try it)

```sh
export MYNAS_CONFIG=$PWD/mynas.json
./mynas init -data ~/nas-data      # asks for a user name and password
./mynas serve
```

Open the printed address (e.g. `http://192.168.1.20:8080/`) in a browser.

## Install as a service

```sh
sudo install -m 755 mynas /usr/local/bin/mynas
sudo useradd --system --home-dir /srv/mynas --shell /usr/sbin/nologin mynas
sudo mkdir -p /srv/mynas && sudo chown mynas:mynas /srv/mynas

sudo mynas init -data /srv/mynas
sudo chown mynas:mynas /etc/mynas/config.json

sudo install -m 644 mynas.service /etc/systemd/system/
sudo systemctl enable --now mynas
journalctl -u mynas            # shows the addresses it is reachable on
```

If you'd rather keep files owned by your own account, set `User=`/`Group=` in
`mynas.service` to your user and run `init` with `-data` pointing to a directory
you own. If share directories are under `/home`, remove `ProtectHome=read-only`.

## Managing users and shares

```sh
mynas user add mom                 # asks for a password
mynas user passwd mom
mynas user del mom
mynas user list

mynas share add photos /mnt/usb/photos                  # everyone, read-write
mynas share add movies /mnt/usb/movies -ro              # everyone, read-only
mynas share add taxes  /srv/mynas/taxes -users florian  # only florian
mynas share del photos                                  # files are kept
mynas share list
```

A running server picks up changes automatically, so you don't need to restart it.
Use `sudo` (or `-c FILE` / `MYNAS_CONFIG`) when the config is in `/etc/mynas`.

## Connecting as a network drive

The WebDAV address is `http://<your-nas>:8080/dav/`. Log in with your mynas user
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

**Windows over plain HTTP:** Windows refuses passwords over unencrypted HTTP
unless you allow it once. In an admin shell, run:

```
reg add HKLM\SYSTEM\CurrentControlSet\Services\WebClient\Parameters /v BasicAuthLevel /t REG_DWORD /d 2 /f
net stop webclient && net start webclient
```

Windows also limits WebDAV downloads to about 50 MB per file by default. You can
raise `FileSizeLimitInBytes` (same registry key, max `0xffffffff`), or use
[rclone](https://rclone.org) or the web UI for very large files.

## Configuration file

`/etc/mynas/config.json` (or `-c FILE`, or `$MYNAS_CONFIG`):

```json
{
  "listen": ":8080",
  "users": [
    { "name": "florian", "password_hash": "$2a$10$..." }
  ],
  "shares": [
    { "name": "files",  "path": "/srv/mynas" },
    { "name": "movies", "path": "/mnt/usb/movies", "read_only": true },
    { "name": "taxes",  "path": "/srv/mynas-taxes", "users": ["florian"] }
  ]
}
```

To change `listen`, edit the file and restart the server. Optional:
`"tls_cert"` and `"tls_key"` (PEM file paths) turn on HTTPS.

## Security notes

mynas is meant for a trusted home network. Traffic is unencrypted plain HTTP
unless you configure TLS. Don't expose mynas directly to the internet. For
access from outside, use a VPN such as WireGuard or Tailscale.

## Layout of the code

| File | What |
|---|---|
| `main.go` | command line (`init`, `serve`, `user`, `share`) |
| `config.go` | config file loading, validation, atomic saving |
| `server.go` | HTTP: login, JSON API for the UI, zip download, WebDAV |
| `fs.go` | the virtual file system: `/` lists shares, each share is an `os.Root` |
| `web/index.html` | the whole web UI, embedded into the binary |
