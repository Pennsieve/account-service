# Compute Node Layers & Environment Provenance — Implementation Steps

Companion to [`compute-node-layers-plan.md`](./compute-node-layers-plan.md) (the *what/why*).
This is the *how/in-what-order* — a reviewable work breakdown. Each milestone lists the
repos/files touched, acceptance criteria, and the decisions it forces. Milestones are
ordered by dependency; several are independently shippable.

> Scope note: the *current* compute-node project (interactive notebooks + delete robustness)
> is done, and layers already work via the **consumer-self-wiring** contract (`read
> LAYERS_DIR → sys.path`). Everything below is the **environments & provenance initiative** —
> a deliberate follow-on, not a hotfix.

---

## Decisions to settle first (gate the build)

These shape multiple milestones; agree on them before M1.

| # | Decision | Options | Leaning |
|---|---|---|---|
| D1 | **Layer versioning** | overwrite-in-place vs immutable snapshot (`{name}@{hash}`) | snapshot for `python-env`/`r-env` (provenance refs them); overwrite OK for `data`, guarded by active-run check |
| D2 | **Compatibility enforcement** | hard-fail vs warn at mount/select on python/R version mismatch | hard-fail at run submit; warn in selector |
| D3 | **Consume-side wiring mechanism** | runtime entrypoint wrapper vs processor self-wiring (status quo) | keep self-wiring as the contract; add the wrapper as the centralized convenience (M3) |
| D4 | **Provenance home** | run record only vs also Discover dataset provenance | run record for v1; Discover later |
| D5 | **EFS import perf** | accept NFS imports vs stage layer → local at task start | accept for v1 (deps-on-base-image posture); revisit if cold-start hurts |
| D6 | **Quota / GC** | per-node layer count/size cap + cleanup policy | define a cap before self-service (M6) |

---

## M1 — Layer type model (foundation)

**Goal:** every layer carries a `layerType`; existing layers default to `data`. No behavior
change yet.

**Tasks**
- workflow-service (`compute-node-layers` store/model): add `layerType` (default `data`),
  `manifestKey`, `version`/`snapshotId`. Migration: backfill existing rows → `data`.
- provisioner `cmd/data-transfer`: `persistent-layer` data-target accepts a `layerType`
  param (alongside `layerName`); `commit-layer` persists it.
- account-service `internal/models/health.go` (`LayerRecord`) + healthchecker reconcile:
  mirror `layerType`.

**Acceptance:** a newly committed layer records its `layerType`; existing layers read back as
`data`; reconcile passes.

**Decisions:** D1 (version field shape).

---

## M2 — Layer manifest (`python-env`)

**Goal:** a built python-env layer self-describes (versions + python version + checksum).

**Tasks**
- processor-build-python-layer: emit `manifest.json` — `pythonVersion`, `platform`,
  `packages` (from `pip freeze`), raw freeze. (Already a README TODO.)
- provisioner `commit-layer`: store the builder's `manifest.json` with the layer; add
  `sizeBytes`/`fileCount` (already computed) + `treeChecksum`. Write `manifestKey` to the record.
- workflow-service: surface `manifestKey` + parsed summary on the layer record / list API.

**Acceptance:** rebuilding `quick-plot-stack` produces a `manifest.json` with resolved
versions + `pythonVersion: 3.12`, readable via the layers API.

**Depends on:** M1. **Decisions:** D1.

---

## M3 — Safe consume-side wiring (runtime wrapper)

**Goal:** a `python-env` layer is importable without consumer hardcoding, **without
clobbering** the image's own `PYTHONPATH`.

**Tasks**
- Decide the wrapper mechanism (sub-decision): a small entrypoint shim the converter prepends
  to the container command, OR an init that writes an env file the processor sources. It must
  **append** `$LAYERS_DIR/{name}/lib/python{ver}/site-packages` (version from the manifest)
  to the existing `PYTHONPATH`, and set `PYTHONPYCACHEPREFIX=/tmp/pycache`.
- provisioner converter (`internal/aslconverter`): pass the resolved env-layer + its manifest
  version to the wrapper (not via an ECS env override — see plan, "Consume-side wiring").
- Compatibility check (D2): refuse/warn when layer `pythonVersion` ≠ the processor base.

**Acceptance:** a processor with a `python-env` layer imports its packages with no hardcoding,
**and** `pennsieve-quick-plot`'s `ENV PYTHONPATH=/app` still works (regression guard).

**Depends on:** M2. **Decisions:** D2, D3.
*(Reference: the clobbering ECS-override approach was prototyped and rejected — provisioner #34.)*

---

## M4 — Run-environment capture + endpoint

**Goal:** every run records the exact environment that executed.

**Tasks**
- provisioner (entrypoint capture, uniform): image digest (`$ECS_CONTAINER_METADATA_URI_V4`),
  mounted layer refs + **copied manifests**, processor source commit, and — interactive only —
  live `pip freeze`. Write `environment.json` to `/mnt/efs/{cni}/{eri}/logs/{nodeId}/`.
- provisioner `cmd/workflow-finalizer`: collect per-node `environment.json` → run-level
  provenance bundle on the run record (workflow-service).
- endpoint: `GET /compute/workflows/runs/{runId}/environment[?nodeId=]` (gateway /
  workflow-service) — mirrors the existing per-node logs endpoint.

**Acceptance:** a completed non-interactive run exposes image digest + layer versions; an
interactive session additionally exposes the live freeze. Survives layer deletion (manifests
are copied, not referenced).

**Depends on:** M2 (manifests to copy). **Decisions:** D4.

---

## M5 — App MVP (view provenance)

**Goal:** users can see a run's environment and browse layer details.

**Tasks**
- pennsieve-app `RunMonitor/RunDetail.vue`: clone the per-node **logs dialog**
  (`openLogs`) into an **Environment** dialog (`openEnvironment` → M4 endpoint); per-node +
  run-level; base image, versions, env layers, searchable package list, download lock.
- pennsieve-app `ComputeNodes/ComputeNodeLayers.vue` + `computeResourcesStore`: **type
  badge** + manifest detail (row-expand).

**Acceptance:** from a run, a user opens "Environment" and sees what ran; the layer catalog
shows type + contents.

**Depends on:** M4 (endpoint), M2 (manifest for the catalog). **Decisions:** —

---

## M6 — Self-service env layers (`requirements.txt` → build)

**Goal:** a user creates a python-env layer from a `requirements.txt` (basic/secure only).

**Tasks**
- account-service / workflow-service: build-trigger API — `{computeNode, layerName,
  requirements.txt, pythonVersion}` → launch the build workflow (`python-layer-builder` +
  `persistent-layer` target) on the node; track build status.
- pennsieve-app `ComputeNodeLayers.vue`: extend the create-layer form to
  `{name, requirements, pythonVersion}`; show a `BUILDING` state on the existing badge.
- Quota/GC enforcement (D6).

**Acceptance:** user submits a `requirements.txt` → layer builds on Fargate → shows READY →
selectable; resolved lock captured (M2). Rejected on compliant nodes with a clear message.

**Depends on:** M1, M2. **Decisions:** D1 (rebuild semantics), D6 (quota).

---

## M7 — Data layers (reference data)

**Goal:** `data` layers are referenceable and show up as inputs, not environment.

**Tasks**
- provisioner converter: per-mounted-`data`-layer `LAYER_<NAME>_DIR` env var.
- provenance: route `data` layers to the **inputs/reference-data** dimension of the run
  record (identity from the manifest — version/source/treeChecksum).
- (compliant) S3-staging path to populate a data layer's EFS dir via the gateway endpoint.

**Acceptance:** a workflow references a genome via `LAYER_GENOME_DIR`; the run's provenance
shows it under reference data by identity.

**Depends on:** M1, M4. **Decisions:** —

---

## M8 — Selection rules + UX

**Goal:** enforce "one env layer per processor; many data layers."

**Tasks**
- pennsieve-app `RunMonitor/LayerNameSelector.vue`: single-select for env layers, multi-select
  for data; compatibility hint (D2).
- workflow-service run submit: validate ≤1 `python-env` and ≤1 `r-env` per processor.

**Acceptance:** selecting two env layers is rejected (UI + API); multiple data layers allowed.

**Depends on:** M1. **Decisions:** D2.

---

## M9 — R env (parity)

**Goal:** `r-env` layers, mirroring python.

**Tasks**
- new `processor-build-r-layer` (`renv`/`pak` → library dir; emit `renv.lock` manifest).
- provisioner converter/wrapper: append to `R_LIBS_USER` (M3 mechanism).
- catalog/selector: R badge + version.

**Acceptance:** an R workflow consumes an `r-env` layer; provenance records `renv.lock` + R version.

**Depends on:** M2, M3. **Decisions:** D2.

---

## M10 — Reproduce + notebook snapshot

**Goal:** close the loop.

**Tasks**
- pennsieve-app: "re-run with this environment" on the RunDetail env dialog → pre-fill run
  launch pinned to the captured image digest + layer versions.
- pennsieve-app `JupyterSession/NotebookSession.vue`: "snapshot environment" / "save as
  layer"; freeze at session end; embed in `.ipynb` metadata.

**Acceptance:** a user re-runs a past run's exact environment; a notebook's environment is
captured at session end and embedded in the saved notebook.

**Depends on:** M4, M5, M6. **Decisions:** D4 (reproduce/provenance scope).

---

## Suggested sequencing

```
M1 ─▶ M2 ─▶ M3 (wiring)        ─┐
            M4 (capture) ─▶ M5 (app view) ─▶ M10 (reproduce)
M1 ─▶ M2 ─────────────────▶ M6 (self-service)
M1 ─────────────────────▶ M7 (data) , M8 (selection)
M2, M3 ─────────────────▶ M9 (R)
```

**First reviewable slice:** M1 + M2 (type model + manifest) — pure foundation, low risk,
unblocks everything. Then split: M3+M4+M5 (the provenance/usability spine) vs M6
(self-service) can proceed in parallel once M2 lands.

---

## Cross-repo summary

| Repo | Milestones |
|---|---|
| workflow-service | M1, M2, M4, M6, M8 |
| compute-node-aws-provisioner-v2 (`data-transfer`, `aslconverter`, `workflow-finalizer`, gateway) | M1, M2, M3, M4, M7, M9 |
| account-service | M1 (model mirror), M6 (build-trigger API) |
| processor-build-python-layer / new processor-build-r-layer | M2, M9 |
| pennsieve-app (`Analysis/`) | M5, M6, M8, M10 |
