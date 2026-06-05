# docs

The mundane website. Plain hand-written HTML, one compiled stylesheet, a few
lines of optional JS. No SSG, no node_modules, no framework. The site is itself
a portable artifact — open `index.html` with `file://` and it works.

GitHub Pages serves this folder. Commit is deploy.

## Files

| File          | What                                                      |
|---------------|-----------------------------------------------------------|
| `index.html`  | The whole page. Single scroll, anchored sections.         |
| `styles.css`  | Compiled Tailwind. **Committed** — no build needed to serve. |
| `tailwind.css`| Source for `styles.css`. Edit this, then rebuild.         |
| `app.js`      | Progressive enhancement only (tabs, theme, copy, scroll-spy). Page reads fine without it. |

## Edit

Change copy or markup in `index.html`. If you touch Tailwind classes, rebuild:

```sh
make docs        # from the repo root
```

That fetches the pinned Tailwind standalone CLI (cached at `docs/.tailwindcss`,
gitignored), scans `index.html` + `app.js`, and writes `styles.css`. No npm.

## Deploy

One-time: **Settings → Pages → Build and deployment → Deploy from a branch →
`main` / `/docs`**. After that, every push to `main` that touches `docs/` ships.

Lands at `https://paulbellamy.github.io/mundane/`.
