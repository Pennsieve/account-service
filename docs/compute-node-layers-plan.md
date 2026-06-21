# Compute Node Layers — Typed Layers, Environments & Run Provenance

## Problem

Compute nodes already support **persistent layers** — read-only EFS directories that
survive across executions and are mounted into runs (built by
[`processor-build-python-layer`](https://github.com/Pennsieve/processor-build-python-layer)
via the `persistent-layer` data-target; see workflow `4c23010f`). Today a layer is an
opaque directory, but layers are used for two fundamentally different things:

1. **Reference data** — e.g. make the human genome (GRCh38) available to a workflow.
2. **Software environments** — a Python (`pip --target`) or R package set added to the
   interpreter's path so processors/notebooks can `import`/`library()` without baking
   everything into the base image.

These are identical at the *storage* layer but diverge in how they are built, wired into
a run, version-checked, and surfaced for provenance. Treating them as one untyped blob
means: no automatic `PYTHONPATH`/`R_LIBS` wiring (every consumer hardcodes the path), no
compatibility checks, and no way to record "what environment produced this result" — the
provenance question that matters most for a scientific platform.

## Goal

- Add an explicit **layer type** (`data` | `python-env` | `r-env`, extensible) that drives
  build, consume-side wiring, manifest schema, compatibility, and UX.
- Make a layer's contents **usable without consumer-side hardcoding** — the platform wires
  `PYTHONPATH` / `R_LIBS` / a data path based on the type.
- Capture, per run (workflow **and** notebook), the **fully resolved environment** that
  executed — base image + env layers + live package freeze — as an immutable provenance
  record, and surface it (and reproduce from it) in the app.
- Cleanly separate **software-environment provenance** from **reference-data provenance**.

## Design Decisions

- **Typed layers**: a `layerType` discriminator on the layer record and the
  `persistent-layer` data-target. `data` is the default (back-compat for existing untyped
  layers); `python-env` / `r-env` are explicit. Extensible (`conda-env`, `julia-env`…).
- **Storage stays generic**: all types are a read-only EFS dir at
  `/mnt/efs/{computeNodeId}/layers/{layerName}`; the type changes the *internal layout* and
  *how it's consumed*, not where it lives.
- **Consume-side wiring is the one real fork**: the ASL converter reads each requested
  layer's `(type, version)` and injects the right env (`PYTHONPATH` / `R_LIBS_USER` /
  `LAYER_<NAME>_DIR`). Today it injects only a generic `LAYERS_DIR`.
- **Provenance = run-level freeze, not layer manifest**: the authoritative record is a
  `pip freeze` / `renv` snapshot of the *live interpreter* at execution time, plus the
  container image digest and the layer refs. Mechanism-agnostic — works whether the env
  came from a layer, the base image, or a runtime install.
- **Two provenance dimensions**: software environment (base image + env layers + freeze)
  vs reference data (data layers). The layer type decides which dimension a layer feeds.
- **Immutability**: env-layer provenance is only meaningful if a layer reference is stable.
  Resolve the current overwrite-vs-snapshot ambiguity in favor of versioned snapshots for
  env layers (see Open Questions).

---

## Layer Type Model

| Type | Internal layout | Built by | Consumed by | Manifest / lockfile | Interpreter-coupled |
|---|---|---|---|---|---|
| **`data`** (default) | opaque file tree | any producer (download/stage, e.g. a genome) | mount + `LAYER_<NAME>_DIR` env var | description, size, source, domain version (e.g. `GRCh38`), optional checksum | No |
| **`python-env`** | `lib/python{ver}/site-packages` (`pip install --target`) | `processor-build-python-layer` | prepend to `PYTHONPATH` (+ `bin/` → `PATH`) | `pip freeze`, `python_version`, `platform`/arch | Yes (wheel ABI) |
| **`r-env`** | R library dir (`library/`) | `processor-build-r-layer` (analogous, `renv`/`pak`) | prepend to `R_LIBS_USER` / `.libPaths()` | `renv.lock`, `r_version`, `platform` | Yes (package ABI) |

### What the type drives

1. **Build** — a type-specific builder processor produces the right internal layout. Python
   exists today; R is a direct analog; `data` is whatever stages the files.
2. **Consume-side wiring** *(the crux)* — the converter branches on type:
   - `data` → expose `LAYER_<NAME>_DIR=$LAYERS_DIR/{name}` (workflows reference reference-data by name).
   - `python-env` → `PYTHONPATH = $LAYERS_DIR/{name}/lib/python{ver}/site-packages : …` (+ `bin/`→`PATH`), and `PYTHONPYCACHEPREFIX=/tmp/pycache` so the read-only, `--no-compile` tree caches bytecode per-task instead of recompiling every import.
   - `r-env` → `R_LIBS_USER = $LAYERS_DIR/{name}/library : …`.
   - Multiple env layers of the same language **stack** in list order.
3. **Manifest schema** — discriminated by type (below).
4. **Compatibility** — only env layers: the layer's `python_version`/`r_version` + platform
   must match the consuming processor's interpreter, or the run fails at import. Validate at
   mount/config and surface in the UI. Data layers have no ABI coupling.
5. **UX** — a type badge; conditional rendering (env → language/version/packages; data →
   size/source/domain version); compatibility hints for env layers only.
6. **Provenance dimension** — env layers feed *software environment*; data layers feed
   *reference data*.

---

## Data model changes

### Layer record (`compute-node-layers`, workflow-service)
Add:
- `layerType: "data" | "python-env" | "r-env"` (default `data`)
- `version` / `snapshotId` (immutable snapshot identifier; see Open Questions)
- `manifestKey` — pointer to the layer's `manifest.json` on EFS/S3

(account-service mirrors the schema for the healthchecker reconcile — see
`internal/models/health.go` `LayerRecord` — and gains `layerType` there too.)

### `persistent-layer` data-target params
Today (`4c23010f`): `defaultParams.layerName`. Add:
- `layerType` — declares the type at commit time; validated against the builder that produced it.

### `manifest.json` (emitted by the builder, stored with the layer)
Common: `{ layerType, layerName, createdAt, builder: {sourceUrl, tag}, sizeBytes, fileCount }`

Type-specific:
- `python-env`: `{ pythonVersion, platform, packages: [{name, version}], pipFreeze: "<raw>" }`
- `r-env`: `{ rVersion, platform, renvLock: {...} }`
- `data`: `{ source, domainVersion?, checksum? }`

The manifest is the keystone: it lets the converter wire correctly (knows the
`python{ver}` subpath), the UI render the catalog, and consumers reproduce.

---

## Run-level environment provenance

Independent of *how* an environment was assembled, capture what actually ran:

1. **At execution time** (processor entrypoint wrapper, uniform across processor + notebook):
   - `pip freeze` / `installed.packages()` of the live interpreter (captures base image +
     all env layers + any runtime install, already resolved).
   - container image **digest** (`$ECS_CONTAINER_METADATA_URI_V4` → `Image` + digest).
   - layer refs (the `Layers` list + each layer's `manifestKey`/version).
   - processor source commit (`sourceUrl` + `tag`).
   - → write `environment.json` (+ `environment.lock`) to `/mnt/efs/{cni}/{eri}/logs/{nodeId}/`.
2. **At workflow end** — the finalizer collects per-node `environment.json` into a run-level
   provenance bundle on the run record.
3. **Notebooks** — the kernel drifts (mid-session `pip install`), so freeze at **session end**
   (session-proxy/teardown) and embed the freeze into the saved `.ipynb` metadata so the
   notebook is self-describing outside Pennsieve.

### Endpoint
`GET /compute/workflows/runs/{runId}/environment[?nodeId=]` — mirrors the existing per-node
logs endpoint (`/runs/{runId}/logs?nodeId=`) the app already uses. Backed by the run
provenance bundle.

### Provenance split
The run record exposes two sections, populated by layer type:
- **Software environment** — base image digest + `python-env`/`r-env` layers + live freeze. *"What code ran."*
- **Reference data** — `data` layers (e.g. genome build). *"What inputs/references it ran against."*

### Reproduce
Lock + image digest = rebuildable. A "re-run with this environment" action pins a new run to
the captured image digest + layer versions (lock as the rebuild fallback).

---

## UX (pennsieve-app, `Analysis/`)

Surfaces already exist; provenance attaches by extending them.

- **`RunMonitor/RunDetail.vue`** *(MVP)* — clone the per-node **logs dialog**
  (`openLogs` → logs endpoint) into an **Environment** dialog (`openEnvironment` → the new
  environment endpoint). Per-node + a run-level summary. Shows base image, python/R version,
  env layers (link to catalog), searchable package list, download lock, and "re-run with this
  environment". Reference-data layers shown in a separate section.
- **`ComputeNodes/ComputeNodeLayers.vue`** — the layers table exists (name/status/size/files);
  add a **type badge** + manifest detail (env → language/version/packages via row-expand; data
  → source/domain version), and "used by N workflows".
- **`RunMonitor/LayerNameSelector.vue`** — filter/group by type; env preview + compatibility
  hint (layer interpreter vs node base) when selecting.
- **`JupyterSession/NotebookSession.vue`** — "snapshot environment" / "save as layer", a drift
  indicator, and `.ipynb` freeze embedding.

---

## Cross-repo touchpoints

| Concern | Repo |
|---|---|
| `layerType` + manifest on the layer record; active-run/layer reconcile | account-service (`internal/models/health.go`, healthchecker) + workflow-service (owner of `compute-node-layers`) |
| `persistent-layer` data-target `layerType` param; `commit-layer`/`mount-layers`; converter env wiring (`PYTHONPATH`/`R_LIBS`/`LAYER_<NAME>_DIR`); run-env capture in `preDestroy`/finalizer; `manifest.json` handling | compute-node-aws-provisioner-v2 (`cmd/data-transfer`, `internal/aslconverter`, `cmd/workflow-finalizer`, gateway) |
| `manifest.json` emission | processor-build-python-layer; new processor-build-r-layer |
| Run-environment endpoint | gateway (`compute-gateway-v2`) / workflow-service |
| Layer catalog, env dialog, selector, notebook snapshot | pennsieve-app (`src/components/Analysis/…`) |

---

## Phasing

1. **Type model** — add `layerType` to the layer record + `persistent-layer` data-target;
   default `data`. No behavior change yet.
2. **Python env, end-to-end** — `manifest.json` from the build processor; converter wires
   `PYTHONPATH`/`PYTHONPYCACHEPREFIX` from `python-env` layers; compatibility check.
3. **Run-environment capture + endpoint** — entrypoint freeze + image digest + layer refs →
   finalizer bundle → `GET /runs/{id}/environment`.
4. **App MVP** — RunDetail Environment dialog (read provenance) + ComputeNodeLayers type badge.
5. **Data layers** — `LAYER_<NAME>_DIR` wiring (trivial — just mount) + reference-data
   provenance section.
6. **R env** — `processor-build-r-layer` + `R_LIBS_USER` wiring + `renv.lock` manifest.
7. **Reproduce + notebook snapshot/`.ipynb` embedding**; optional Discover provenance.

---

## Open Questions

1. **Immutability/versioning** — `commit-layer` currently overwrites; the README claims
   layers are immutable. For env layers (where run provenance references them) pick versioned
   snapshots (`{name}@{hash}`) and have the converter pin consumers; data layers may tolerate
   overwrite. Decide and make consistent.
2. **Compatibility enforcement** — hard-fail vs warn-and-proceed on python/R version or arch
   mismatch at mount/config.
3. **EFS import latency** — env layers imported off NFS are slow; position as incremental deps
   on a curated base image, and/or stage to local ephemeral storage at task start. Decide
   whether to invest in local staging now.
4. **Compliant mode** — builds can't reach PyPI/CRAN (no internet); needs a private mirror
   (VPC endpoint / S3 index) or pre-built layers.
5. **Provenance home** — run record only, or promote the environment + reference-data record
   into the published dataset's provenance in Discover (highest reproducibility value).
6. **Reproduce scope** — capture-only, or a first-class "rebuild environment from a run's lock"
   action.
