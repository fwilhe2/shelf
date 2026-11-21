# Your real files in shelf

How to move your existing files into shelf running in the container, check
that they all arrived, and keep them safe afterwards. This assumes the Podman
setup from the [README](../README.md#install-podman-rootless-eg-fedora-silverblue);
the Kubernetes notes are [at the end](#kubernetes).

## Where the files are: a directory, not a volume

The container does not keep your files inside a Podman volume or an image
layer. `deploy/shelf.container` **bind-mounts** two ordinary directories of the
host:

| Host | In the container | What |
|---|---|---|
| `~/nas-data` | `/data` | your files, the share `files` |
| `~/.config/shelf` | `/config` | `config.json`, TLS files, the search index |

So a file you see in the web UI as `files/taxes/2025.pdf` is the host file
`~/nas-data/taxes/2025.pdf`, byte for byte. Because of `UserNS=keep-id` it
belongs to your own user. You can stop the container, remove the image, or
delete shelf entirely, and the files are still there and still readable with
any tool. shelf has no database of files that could get out of step with them.
The search index in `/config/search` is the only derived data, and
`shelf index` can rebuild it at any time.

This means "how trustworthy is the volume" is really "how trustworthy is the
host's disk and file system". See [Keeping the files safe](#keeping-the-files-safe).

### Put the data on its own disk

For real data, keep `/data` on a disk of its own rather than in your home
directory on the system disk. That way a reinstall of the OS cannot touch it,
and the disk can be moved to another machine. Mount it, say, at
`/var/mnt/nas` (Silverblue: `/var/mnt` is writable), make it yours, and point
the unit at it:

```sh
sudo mkdir -p /var/mnt/nas && sudo chown $USER: /var/mnt/nas
# ~/.config/containers/systemd/shelf.container
#   Volume=/var/mnt/nas:/data:z
systemctl --user daemon-reload && systemctl --user restart shelf
```

Add the disk to `/etc/fstab` with `nofail`. That way a missing disk does not
stop the boot, and shelf shows the share as missing instead of writing into the
empty mount point.

## Importing files

**The easiest and fastest way is to copy on the host itself**, straight into
the directory. Nothing goes through the network or through shelf, and shelf
does not need to be stopped. It shows new files as soon as they are there.

```sh
# from another disk plugged into the NAS
rsync -a --info=progress2 /run/media/$USER/olddisk/Documents/ ~/nas-data/documents/
```

- The trailing `/` on the source copies its *contents* into `documents/`.
- `-a` keeps modification times, which you want: the web UI sorts and shows
  them, and backups rely on them.
- An interrupted `rsync` can simply be run again; it continues where it
  stopped and skips what is already there.

**From another computer**, use `rsync` over SSH to the NAS host. It is much
faster and more robust than uploading through the browser:

```sh
rsync -a --info=progress2 ~/Pictures/ nas.local:nas-data/photos/
```

If SSH is not available, `rclone copy` over WebDAV
(`rclone config` → WebDAV → `https://nas.local:8443/dav/`) is the next best
choice. It retries failed files and can check the copy (`rclone check`). The
web UI's *Upload folder* works too, but is meant for occasional uploads, not
for hundreds of gigabytes.

### Use cp or rsync, not mv (SELinux)

On Fedora, SELinux allows the container to read only files labelled
`container_file_t`. A file **created** in the data directory (by `cp`,
`rsync`, an upload) gets that label from the directory. A file **moved** in
with `mv` from somewhere else in your home keeps its old label
(`user_home_t`). The running container cannot read it: the web UI lists it,
but opening it fails. Podman relabels the whole directory when the container
starts, so a restart fixes it, or relabel just the moved files:

```sh
chcon -R -t container_file_t ~/nas-data/the-moved-folder
```

The relabel on start walks every file, so with a very large tree the first
start can take a while.

### Files that are already where they should be

You do not have to copy files into `~/nas-data` at all. A directory that already
holds your photos can be shared where it is. Add a `Volume=` line for it to
the unit and a share in shelf:

```sh
# shelf.container:   Volume=/var/mnt/photos:/photos:z
systemctl --user daemon-reload && systemctl --user restart shelf
podman exec -it shelf /shelf share add photos /photos -ro
```

**Make an archive read-only** (`-ro`, or `"read_only": true` in the
config) when nobody needs to change it through shelf. Then no one can delete
or overwrite a file in it from the web UI or WebDAV, even by accident.
Do not bind-mount your whole home directory, or a directory that other
services also use: `:z` relabels everything under it.

### Check that everything arrived

Before you delete the originals, compare by content, not by size and date:

```sh
rsync -a --checksum --dry-run --itemize-changes /run/media/$USER/olddisk/Documents/ ~/nas-data/documents/
```

No output means every file is there and identical. Each line it prints names a
file that is missing or differs.

### Then: index for search

New files are found by their name right away, and by their text once indexed.
The first index of a large import reads every document and runs OCR on every
scan and photo, which can take hours on a small machine. shelf indexes by itself within the hour, or start it right away
with *Update now* in the web UI's *Search index* dialog, or by hand:

```sh
podman exec shelf /shelf index
```

## Keeping the files safe

Not trusting a single place with your only copy is the right instinct. There
are three different risks, and each one needs its own protection.

### 1. shelf itself damaging a file

What shelf does to your files:

- **It only writes when you ask it to**: an upload, new folder, rename, move or
  delete from the web UI or WebDAV. It never rewrites, converts or "imports"
  files. Viewing, downloading, zipping and indexing only read them. clerk, the
  indexer, only reads the shares and keeps its index in `/config`.
- **Uploads are all-or-nothing.** An upload is written to a hidden
  `.shelf-upload-…` file next to its destination, flushed to the disk, and only
  then renamed over the old file. If the connection drops, the phone goes to
  sleep, or the power fails during an upload, the old file stays as it was. A
  leftover `.shelf-upload-…` file after a power cut is a partial upload and can
  be deleted.
- **`config.json` is written the same way** (temporary file, flush, rename), so
  `shelf user add` cannot leave a half-written config.
- **Deleting is immediate; there is no trash.** A file deleted in the web UI or
  through a mounted drive is gone from the disk. Your backups are the undo, so
  set them up before you put real data in. Use read-only shares for files that
  should never change.
- shelf cannot reach outside its shares: `..` paths and symlinks pointing out
  of a share are refused.

### 2. The disk or file system damaging a file

This is the risk that remains, and it is the same for any NAS: a disk that
slowly fails, a bad cable, or a power cut during a write. Two things matter:

**A file system that notices damage.** ext4 and XFS store your data without
checksums. If a bit flips on the disk, the file is silently different, and
every backup made afterwards may copy the damaged version. **btrfs** (the
default on Fedora, including Silverblue) checksums every block. A damaged
block is reported as a read error instead of being returned as wrong data.
With two disks in btrfs RAID 1 it is even repaired from the good copy. Check
the whole disk regularly:

```sh
sudo btrfs scrub start -B /var/mnt/nas    # reads and verifies every block
sudo btrfs device stats /var/mnt/nas      # error counters, should all be 0
```

Fedora does not schedule scrubs on its own. Run one monthly, for example with a
systemd timer or `btrfsmaintenance`. Also watch the disk's own health with
`smartctl -a /dev/sdX` (package `smartmontools`).

**An uninterruptible power supply**, if the NAS is a desktop machine. It is
cheaper than losing a day of work to a corrupted directory.

### 3. Losing the disk: backups

RAID is not a backup: it copies a deletion or a ransomware encryption to both
disks instantly. The rule of thumb is **3 copies, on 2 different disks, 1 of
them somewhere else**.

`deploy/shelf-backup` (see the [README](../README.md#automatic-backups)) makes
a daily rsync snapshot of `~/.config/shelf` and `~/nas-data` on another disk.
It keeps the last 14 days, so a file deleted by mistake can be restored for two
weeks. Some things to know about it:

- Each snapshot looks like a full copy, but unchanged files are **hard links**:
  on the backup disk, a file that never changed exists only *once*, shared by
  all snapshots. If that one copy is damaged, it is damaged in every snapshot.
  So the backup disk should be btrfs too, and get a scrub.
- rsync decides what changed by size and modification time. A file silently
  damaged on the NAS disk keeps both, so it is **not** copied again, and the
  backup keeps the good version. That is good, but nothing *tells* you about
  the damage. Only a btrfs scrub, or a checksum list as below, would.
- Edit `BACKUP_SOURCES` in `backup.env` when you add shares outside
  `~/nas-data`.
- Keep a second backup disk off-site (at work, with family) and swap it now and
  then; or add an off-site copy with `restic` or `borg` to a remote machine.

**A checksum list** detects damage on any file system and works for the
backup disk as well:

```sh
cd ~/nas-data && find . -type f ! -name '.shelf-upload-*' -print0 | sort -z \
  | xargs -0 sha256sum > ~/.config/shelf/checksums.sha256     # once, and after big imports
cd ~/nas-data && sha256sum --check --quiet ~/.config/shelf/checksums.sha256
```

`--check` prints only files that differ or are missing. Files you changed on
purpose show up too, so recreate the list after you change files.

**Test a restore** once: copy a folder back from a snapshot to a temporary
directory and open some files. A backup that was never restored is a hope, not
a backup.

## Kubernetes

On Kubernetes, `/data` is a PersistentVolumeClaim (`deploy/kubernetes/pvc.yaml`),
and everything above depends on what the StorageClass provides. With
`local-path` or a `hostPath`/`local` PersistentVolume, the volume is a
directory on the node, and the advice above applies to that node's disk as it
stands. With network storage (Longhorn, Ceph, NFS), checksums, replicas and
snapshots are that system's job, so check what yours does.

To import, copy on the node into the volume's directory
(`kubectl get pv -o yaml` shows the path), and make the files owned by uid/gid
65532 (`chown -R 65532:65532`). Or use `rclone copy` over WebDAV from any
machine. `kubectl cp` into the shelf pod does not work: the image is
distroless and has no `tar`.
