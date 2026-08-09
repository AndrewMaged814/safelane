# SafeLane Studio UI prototype

Throwaway prototype for [Design the minimum Phase 1 SafeLane Studio](https://github.com/AndrewMaged814/safelane/issues/8).

> **Superseded behavior:** this prototype predates the v3.2 safety-case screen and still contains
> profile creation, incident, and Prometheus concepts that are outside the pre-final runtime. Reuse
> visual styling only. [`docs/safelane-studio.md`](../../docs/safelane-studio.md) is authoritative.

Run from the repository root:

```powershell
python -m http.server 4173 --directory prototypes/safelane-studio
```

Open <http://localhost:4173/?page=changes>.

- `?page=changes` — unresolved and resolved PR assessments
- `?page=assessment` — one-column assessment details
- `?page=profiles` — built-in and custom rollout profiles
- `?page=create&mode=ai` — one-shot local AI profile generation

This code is intentionally throwaway. Navigation and actions work in browser memory. It does not write `policy.yaml` or deploy anything.
