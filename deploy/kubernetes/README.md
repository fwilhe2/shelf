# shelf on Kubernetes

The same container image and hardening as the Podman setup: non-root
(uid/gid 65532), a read-only root file system, no capabilities, and the
`restricted` Pod Security Standard on the namespace. CI publishes the image for
amd64 and arm64 (Raspberry Pi) as `ghcr.io/fwilhe2/shelf`: `:latest` from
`main`, rebuilt weekly with Debian's updates, `:sha-<commit>` for every commit
on `main`, and `:1.2.3` / `:1.2` for a tag `v1.2.3`.

| File | What |
|---|---|
| `kustomization.yaml` | everything below, in the namespace `shelf`; the image tag |
| `pvc.yaml` | two volumes: `shelf-config` → `/config`, `shelf-data` → `/data` |
| `deployment.yaml` | one shelf server |
| `service.yaml` | port 8080, inside the cluster |
| `index-cronjob.yaml` | `shelf index` every hour, for search |
| `init-pod.yaml` | `shelf init`, once, to create the config |

Both volumes come from the cluster's default StorageClass. To keep the files on
a particular disk, see the comment at the top of `pvc.yaml`.

## First start

```sh
kubectl apply -k deploy/kubernetes

# create the config and the first user: type the user name, Enter, then the
# password twice (attach may miss the first prompt)
kubectl create -f deploy/kubernetes/init-pod.yaml
kubectl -n shelf attach -it shelf-init
kubectl -n shelf delete pod shelf-init

# the server waits for the config; start it now instead of at its next retry
kubectl -n shelf rollout restart deployment/shelf
kubectl -n shelf logs deployment/shelf
```

Try it with `kubectl -n shelf port-forward svc/shelf 8080` and
<http://localhost:8080/>. To reach shelf from other devices, change the Service
to `type: LoadBalancer` or `NodePort` (`service.yaml`).

## Users, shares and HTTPS

The commands from the main README run in the server's pod; changes apply
without a restart.

```sh
alias shelf='kubectl -n shelf exec -it deployment/shelf -- /shelf'
shelf user add mom
shelf share add photos /data/photos
```

Share paths are inside the container: to share another disk, add a volume to
`deployment.yaml` (and to `index-cronjob.yaml` for search).

For HTTPS, list the names and IPs devices use to reach shelf, then restart it:

```sh
shelf cert -hosts nas.local,192.168.1.20
kubectl -n shelf rollout restart deployment/shelf
```

shelf then serves HTTPS on the same port, 8080 (unless you change `listen` in
`/config/config.json`, and `containerPort` with it).

## Updates and backups

`kubectl -n shelf rollout restart deployment/shelf` pulls the newest
`:latest`. To pin a version, set `newTag` in `kustomization.yaml`.

Back up both volumes. The files are plain files, so any tool that can read the
volume works, e.g. a snapshot from your storage, or `deploy/shelf-backup` on
the node for `hostPath`/`local` volumes.
