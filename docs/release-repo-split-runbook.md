# Release repository split: owner runbook

**Status: executed on 2026-10-12.** `monoes/mono-agent-src` (source, private) and `monoes/mono-agent`
(public releases) are live; v0.134.1 was the first release built in the renamed source and published
to the public repository with `RELEASES_REPO_TOKEN`, its signed manifest verified. Still open: replace
the broad `RELEASES_REPO_TOKEN` (today the owner's classic token) with a fine-grained token limited to
`monoes/mono-agent` (Contents: read and write). File source issues and PRs in `monoes/mono-agent-src`,
never in `monoes/mono-agent`.

Decision (owner, 2026-10-12), "option B, swap the names":

| Name | Value | Meaning |
|---|---|---|
| `SOURCE_REPO` | `monoes/mono-agent-src` | today's `monoes/mono-agent`, renamed, and made private last |
| `RELEASES_REPO` | `monoes/mono-agent` | a NEW public repository that holds releases, `install.sh`, a README and the issue templates, and nothing else |
| `RELEASES_BRANCH` | `main` | default branch of `RELEASES_REPO` |

Installed clients keep asking `https://api.github.com/repos/monoes/mono-agent/releases/latest`,
`https://raw.githubusercontent.com/monoes/mono-agent/HEAD/install.sh` and
`https://github.com/monoes/mono-agent/releases/download/...`. Those names must end up
answering from the new public repository. The Go module path stays `github.com/monoes/mono-agent`.

`.github/workflows/release.yml` already works in both worlds (the code merged first, the
repositories change afterwards): it creates the release in `RELEASES_REPO` (workflow-level `env`),
using `secrets.RELEASES_REPO_TOKEN` and, only while `RELEASES_REPO` is the very repository the
workflow runs in, the job's own token. Before the rename nothing here is needed; the workflow
behaves as it did, except that it now writes through the token fallback.

Only you can run this: nothing below is run by an agent or by CI. Every command is a `gh` or
`git` command against GitHub. Steps marked **[IRREVERSIBLE]** cannot be taken back (or only with
a loss); **[POINT OF NO EASY RETURN]** can be undone only with the rollback in section 4.

```bash
SRC=monoes/mono-agent-src       # SOURCE_REPO
REL=monoes/mono-agent           # RELEASES_REPO
WORK=$HOME/scratch/split        # scratch space (the main disk, never /tmp)
mkdir -p "$WORK"
gh auth status                  # an owner-level login with admin on monoes/mono-agent
```

## 0. Before you start (nothing changes yet)

1. This branch is merged to `master` and at least ONE release has run green with it, before
   the rename. That proves the new workflow in the "both names are the same repository" mode
   (the `Check the release repository can be written` step passes on `github.token`, the
   release lands in `monoes/mono-agent`, `sign-manifest` and `publish-releases-repo` finish).
   `sign-manifest` and `publish-releases-repo` run only when the secret `RELEASES_REPO_TOKEN` exists
   (as they ran only when the old `RELEASES_REPO` secret existed); without it this release proves
   the `release` job alone, and the signed path is first exercised in 1.7.
2. The LICENSE is final. The public repository gets a copy of `LICENSE`, so merge the
   relicensing branch (`feat/proprietary-license`) first.
3. Freeze `master`: no merges and no pushes from people or agents until section 1 is done.
   A release run that starts in the gap fails fast at `Check the release repository can be
   written` (see "What breaks in between"), but it has already pushed its tag.
4. Inventory what the source repository holds, so you can compare after the rename:

   ```bash
   gh secret list -R monoes/mono-agent
   gh secret list --env release -R monoes/mono-agent       # RELEASE_SIGNING_KEY lives here
   gh variable list -R monoes/mono-agent                   # ACCOUNT_ENFORCEMENT_*, RELEASE_SIGNING_BOOTSTRAP
   gh api repos/monoes/mono-agent/environments --jq '.environments[].name'
   gh api repos/monoes/mono-agent/hooks --jq '.[].config.url'
   gh api repos/monoes/mono-agent/keys --jq '.[].title'
   gh api repos/monoes/mono-agent --jq '{private,has_discussions,has_pages,stargazers_count,forks_count}'
   ```

5. Download the latest release, so the republish in section 1 takes seconds:

   ```bash
   TAG=$(gh release view -R "$REL" --json tagName -q .tagName)
   mkdir -p "$WORK/latest" && gh release download "$TAG" -R "$REL" -D "$WORK/latest"
   gh release view "$TAG" -R "$REL" --json body -q .body > "$WORK/notes.md"
   ls "$WORK/latest"    # binaries, SHA256SUMS.txt, SHA256SUMS, manifest.json, manifest.json.sig, NOTICE, ...
   ```

6. Stage the seed for the public repository (nothing is pushed yet). Read `seed/SECURITY.md`,
   `seed/SUPPORT.md` and the two docs once afterwards: they were written for the source tree and
   may link to files that are not in the public repository.

```bash
cd "$WORK" && rm -r -f seed && mkdir -p seed/docs seed/.github/ISSUE_TEMPLATE
SRCTREE=$HOME/projects/monoes/mono-agent      # a checkout of master with this change
cp "$SRCTREE"/{install.sh,LICENSE,NOTICE,SECURITY.md,SUPPORT.md} seed/
cp "$SRCTREE"/docs/{USAGE_POLICY.md,COMPARISON.md} seed/docs/
cp "$SRCTREE"/.github/ISSUE_TEMPLATE/*.yml seed/.github/ISSUE_TEMPLATE/
# The templates link to files that exist in the public repository only:
sed -i 's|blob/main/AGENTS.md|blob/main/README.md|; s|blob/master/docs/COMPARISON.md|blob/main/docs/COMPARISON.md|' \
    seed/.github/ISSUE_TEMPLATE/*.yml
cat > seed/README.md <<'EOF'
# MonoAgent releases

Binaries and checksums of MonoAgent (`monoagentcli` and the desktop app).

- Install (macOS, Linux): `curl -fsSL https://raw.githubusercontent.com/monoes/mono-agent/HEAD/install.sh | bash`, then `monoagentcli account login`. Every download is verified against the signed release manifest and `SHA256SUMS.txt`.
- Update: `monoagentcli update`. Third-party licenses: `NOTICE`, attached to every release.
- Questions and bugs: this repository's Discussions and Issues. Security: SECURITY.md or security@monoes.me.
- This repository publishes releases only. The source is private and does not take contributions.
EOF
```

7. Recommended rehearsal (plan Step 10): a scratch private repository with a copy of the source and a
   scratch public repository as its `RELEASES_REPO` (edit the workflow-level `env` there only), its
   own `RELEASES_REPO_TOKEN`, and one push to its `master`. The publish steps (`softprops` with
   `repository:` and `token:`, the `install.sh` upload, `publish-releases-repo`) are exactly what no
   test here can run. Delete the scratch repositories afterwards.
8. Check the plan before closing: a private repository uses the account's included Actions minutes,
   and `release.yml` and `ci.yml` run macOS and Windows jobs (GitHub's published per-minute rates:
   Linux $0.006, Windows $0.010, macOS $0.062). A spending limit of $0 stops the release.

## 1. The swap (do 1.1 to 1.7 in one sitting; 1.3 and 1.4 take minutes)

**1.1 Rename the source.** Reversible only until 1.4 creates a repository under the old name.

```bash
gh repo rename mono-agent-src -R monoes/mono-agent --yes
gh repo view "$SRC" --json nameWithOwner,visibility -q '.nameWithOwner+" "+.visibility'   # monoes/mono-agent-src PUBLIC
```

Everything that belongs to the repository moves with it, automatically: the code and tags,
issues, pull requests, Discussions, stars, forks, branch protection and rulesets, collaborators,
webhooks, deploy keys, Dependabot, Actions run history, **repository secrets and variables, and
the `release` environment with its reviewers, deployment branch policy and its secret
`RELEASE_SIGNING_KEY`**. Nothing there is recreated. Re-run the inventory from 0.4 against `$SRC`
and compare. Old URLs (`github.com/monoes/mono-agent...`, the API and raw URLs) now redirect to
`$SRC`, which is still public, so installed clients keep working.

**1.2 Point every local clone at the new name, BEFORE 1.4.** After 1.4 the old name belongs to the
public releases repository, and a clone whose `origin` still says `monoes/mono-agent` would push
the source into the public repository.

```bash
# find the clones (worktrees share their main clone's remote)
find "$HOME/projects" "$HOME/scratch" -maxdepth 5 -name .git -prune 2>/dev/null | while read -r g; do
  d=$(dirname "$g"); printf '%s  ' "$d"; git -C "$d" remote get-url origin 2>/dev/null || echo "(no origin)"
done
# for each clone that prints monoes/mono-agent(.git) (https or git@github.com:), keep its URL style:
git -C ~/projects/monoes/mono-agent remote set-url origin git@github.com:monoes/mono-agent-src.git
git -C ~/projects/monoes/mono-agent remote -v
gh repo set-default "$SRC"     # run inside each clone that uses gh
```

Do the same on every other machine (the Mac under `/Users/morteza/...`), in CI scratch clones, and
for any agent worktree created from a remote URL. Notes and plans that say `monoes/mono-agent` for the source mean `$SRC` from now on. The Edge unpacked extension loads from the main
checkout; check it for unpushed work first, and it is unaffected by the rename itself.

**1.3 Workflows.** No commit is needed for the rename: `release.yml` reads `github.repository` where it
needs the running repository. Confirm with `gh workflow list -R "$SRC"` that the workflows are listed.

**1.4 Create the public releases repository. [POINT OF NO EASY RETURN]** From here the old name
stops redirecting to `$SRC` and answers from the new repository, which is empty until 1.6.

```bash
gh repo create "$REL" --public --description "MonoAgent releases (binaries and checksums)"
gh repo edit "$REL" --enable-issues --enable-discussions --enable-wiki=false
gh api -X PUT "repos/$REL/private-vulnerability-reporting"
gh repo clone "$REL" "$WORK/public" && cd "$WORK/public"
git checkout -b main 2>/dev/null || true
cp -R "$WORK"/seed/. .
git add -A && git commit -m "docs: README, install.sh, SECURITY, SUPPORT and issue templates"
git push -u origin main
gh repo edit "$REL" --default-branch main
gh repo view "$REL" --json defaultBranchRef,visibility -q '.defaultBranchRef.name+" "+.visibility'   # main PUBLIC
```

Do not enable branch protection that requires pull requests on `main`: the workflow commits
`install.sh` there with the token (a ruleset that exempts the token's owner is fine). Create the
labels you want from `.github/labels.md` in the new repository if the templates use them.

**1.5 Make the first release visible again.** Until this step `releases/latest` answers 404 and
the manifest's asset URLs 404 (see "What breaks in between").

```bash
gh release create "$TAG" "$WORK"/latest/* -R "$REL" --target main --title "$TAG" \
  --notes-file "$WORK/notes.md" --latest
```

Optional, so old release pages and pinned download links keep working (newest last):

```bash
gh release list -R "$SRC" --limit 1000 --json tagName -q '.[].tagName' | tac | while read -r t; do
  [ "$t" = "$TAG" ] && continue
  d="$WORK/old/$t"; mkdir -p "$d"; gh release download "$t" -R "$SRC" -D "$d"
  gh release view "$t" -R "$SRC" --json body -q .body > "$d.md"
  gh release create "$t" "$d"/* -R "$REL" --target main --title "$t" --notes-file "$d.md" --latest=false
done
```

(`sign-manifest` takes its version floor from the newest `vX.Y.Z` release in `$REL`, so the
newest release must be the one you just republished.)

**1.6 Create the fine-grained token and store it. [secret handling]** In the browser:
github.com, Settings, Developer settings, Fine-grained personal access tokens, Generate:
resource owner `monoes`; repository access "Only select repositories" and choose
**`monoes/mono-agent` only**; permission **Contents: Read and write** (Metadata is added on its
own); an expiry you will honour (put the rotation date in your calendar). If the organization asks
for approval of fine-grained tokens, approve it in the organization's settings. Then store it in the
SOURCE repository; the command prompts for the value, so it never lands in your shell history:

```bash
gh secret set RELEASES_REPO_TOKEN -R "$SRC"        # paste the token at the prompt
gh secret list -R "$SRC" | grep RELEASES_REPO_TOKEN
```

The old repository secret `RELEASES_REPO` (which held a repository name, it is no longer read)
can go: `gh secret delete RELEASES_REPO -R "$SRC"`.

**1.7 Unfreeze `master` and run a real release.** Merge a small change through the usual route
(the `release` environment still asks you to approve). Then run the checks in section 3. Do not
continue to section 2 until they all pass on a release that was built by the renamed repository.

## 2. Make the source private [IRREVERSIBLE]

Only after 1.7 has shown a release in `$REL` that the renamed repository published, and section 3 is green.

Know what you give up (GitHub's documentation on changing a repository's visibility is
authoritative; read it first): public forks are detached from the network, stars and watchers
are removed, GitHub Pages stops being published, and the repository's Discussions and Issues
stop being public. Going back to public does not bring them back. Issue and pull request
numbers keep existing in `$SRC`, but links such as `github.com/monoes/mono-agent/issues/375`
(in handoffs, commit messages and `monoes/mono-agent#NNN` comments) now name the public
repository's own, empty, namespace.

```bash
gh repo edit "$SRC" --visibility private --accept-visibility-change-consequences
gh repo view "$SRC" --json visibility -q .visibility        # PRIVATE
```

Then run section 3 again, plus: `curl -s -o /dev/null -w '%{http_code}\n' https://github.com/monoes/mono-agent-src`
must print `404` (anonymous), and one more real release must come out of the private repository.

## 3. Verification (plan Step 12)

Run these anonymously where possible (no `gh` login in the environment, or `curl` as shown).

```bash
REL=monoes/mono-agent; TAG=<the tag the last release made>

# the release exists in the public repo with binaries, both checksum files, the signed manifest and NOTICE
gh release view "$TAG" -R "$REL" --json assets --jq '.assets[].name'
# the API answers for the OLD name the clients use, with the new tag
curl -fsSL https://api.github.com/repos/monoes/mono-agent/releases/latest | jq -r .tag_name
# install.sh is served from the raw URL the README and the docs use
curl -fsSI https://raw.githubusercontent.com/monoes/mono-agent/HEAD/install.sh | head -1      # 200
# a download URL from the manifest resolves
curl -fsSIL "https://github.com/monoes/mono-agent/releases/download/$TAG/SHA256SUMS.txt" | grep -i '^HTTP'
# the signature on the published manifest verifies with the pinned key
mkdir -p "$WORK/v" && cd "$WORK/v" && gh release download "$TAG" -R "$REL" -p manifest.json -p manifest.json.sig -p SHA256SUMS.txt --clobber
monoagentcli release verify manifest.json manifest.json.sig
# an installed client sees it (use a scratch HOME so no real state moves)
HOME="$WORK/h" monoagentcli update --check
HOME="$WORK/h" monoagentcli doctor --check core.update
# a fresh install through the README one-liner
curl -fsSL https://raw.githubusercontent.com/monoes/mono-agent/HEAD/install.sh | INSTALL_DIR="$WORK/bin" bash
"$WORK/bin/monoagentcli" version
```

Also check by hand: the Discussions tab and the issue chooser of `$REL` open and show the
templates; `https://github.com/monoes/mono-agent/security/advisories/new` opens (SECURITY.md);
`monoagentcli` crash reporting (`MONOAGENT_CRASH_REPORT=1`) targets `$REL` (it is the only
repository the binary names).

## What breaks in between

| Window | What happens | Why it is safe |
|---|---|---|
| After 1.1, before 1.4 | Nothing: the old name redirects to `$SRC`, which is still public. | Redirect, same data. |
| After 1.4, before 1.5 | `releases/latest` is 404 and the manifest's `github.com/monoes/mono-agent/releases/download/...` URLs 404, so `update`, `update --app`, `doctor`'s `core.update` and `install.sh` report an error or "no update" and install nothing. The monoes.me manifest copy still answers but its asset URLs fail. | Clients verify before installing and stop on a failed download: no partial install. Minutes, because the assets are already downloaded (0.5). |
| After 1.4, before 1.6 | A release run on `master` pushes its tag in `$SRC`, then stops at `Check the release repository can be written` ("RELEASES_REPO_TOKEN is not set"). The tag exists without a release; the next run takes the next version number. | Nothing is published half-way. Freezing `master` avoids it. |
| After 1.6, before 1.7 | Fine. The first release uses the token. | |
| Old links | `monoes/mono-agent/issues/N`, `/pull/N`, `/blob/...`, `/actions/...` and raw `data/schemas/...` URLs no longer reach the source (see the dependency list in the change description). | Cosmetic for docs; the `$schema`/`$id` URLs are an editor convenience. |
| Installed clients older than this change | They ask `api.github.com/repos/monoes/mono-agent/releases/latest`, which now answers from `$REL`. | This is the point of option B. They need the republished release (1.5). |

## 4. Rollback

* **Before 1.4** (nothing created under the old name): `gh repo rename mono-agent -R monoes/mono-agent-src --yes`, then set every
  clone's `origin` back (1.2). Done; nothing was published differently.
* **After 1.4, before section 2:** keep the evidence and do not delete anything.
  1. `gh repo rename mono-agent-releases-aborted -R monoes/mono-agent --yes` (the public repository moves out of the way; its releases stay in it).
  2. `gh repo rename mono-agent -R monoes/mono-agent-src --yes` (the source takes its name back; redirects work again).
  3. Set the clones' `origin` back to `monoes/mono-agent`. `release.yml` works in the same-repository
     mode with `github.token`; the secret `RELEASES_REPO_TOKEN` is then unused (delete it:
     `gh secret delete RELEASES_REPO_TOKEN -R monoes/mono-agent`).
  4. Make the last release visible in the source again if `releases/latest` there is not the newest one:
     `gh release create "$TAG" "$WORK"/latest/* -R monoes/mono-agent --title "$TAG" --notes-file "$WORK/notes.md"` (only if it is missing).
  5. Delete `monoes/mono-agent-releases-aborted` only when you are sure: **[IRREVERSIBLE]** `gh repo delete monoes/mono-agent-releases-aborted --yes` (needs `gh auth refresh -s delete_repo`).
* **After section 2:** treat the private visibility as one-way. To undo it, `gh repo edit "$SRC" --visibility public --accept-visibility-change-consequences`
  makes the code public again but does not restore stars, watchers or detached forks. For a broken
  release run, fix forward instead: a hand-made release is `gh release create vX.Y.Z <files> -R monoes/mono-agent --title vX.Y.Z --notes-file notes.md`
  (the signed `manifest.json` and `manifest.json.sig` come from `docs/release-runbook.md`, "Break-glass: sign locally").
* **Token trouble:** a leaked or expired `RELEASES_REPO_TOKEN` is replaced, not worked around: revoke it in the GitHub
  settings, create a new one (1.6) and `gh secret set RELEASES_REPO_TOKEN -R "$SRC"` again. Rotate on the date you chose.

## What travels, what is recreated

| Item | After the rename | Action |
|---|---|---|
| Repo secrets and variables of `$SRC` (`RELEASES_REPO_TOKEN`, `ACCOUNT_ENFORCEMENT_RELEASE`, `ACCOUNT_ENFORCEMENT_MIN_DAYS`, `RELEASE_SIGNING_BOOTSTRAP`) | stay with `$SRC` | `RELEASES_REPO_TOKEN` is replaced by the new token (1.6); the unused `RELEASES_REPO` secret is deleted |
| Environment `release` (reviewers, deployment branch `master`) and its secret `RELEASE_SIGNING_KEY` | stay with `$SRC` | none |
| Branch protection, rulesets, CODEOWNERS, collaborators, webhooks, deploy keys, Dependabot | stay with `$SRC` | none |
| Issues, PRs, Discussions, stars, forks | stay with `$SRC` | not copied to `$REL`; lost to the public by section 2 |
| Local clone remotes (every machine) | redirect until 1.4, then wrong | 1.2, before 1.4 |
| Anything else keyed by the name `monoes/mono-agent` (Slack or other integrations, `gh` defaults, GitHub App settings that list repositories by name, the monoes-landing site's links) | the name now means `$REL` | check each; the landing site is a separate repository and out of scope here |
| `$REL` itself | brand new | README, install.sh, SECURITY, SUPPORT, issue templates (1.4); Issues, Discussions and private vulnerability reporting enabled (1.4); no Actions, no secrets |

## Other things that name `monoes/mono-agent` (they change meaning at 1.4)

* `$schema` in `data/automation-templates/**` and `$id` in `data/schemas/*.schema.json`, and
  `SchemaBaseURI` in `internal/schemagen/schemas.go`: `raw.githubusercontent.com/monoes/mono-agent/master/data/schemas/...`.
  The public repository has neither that path nor a `master` branch, so editors that fetch the
  schema get 404. Decide where the schemas are served (monoes.me, or the public repository) in a
  follow-up; the files are embedded, so nothing in the binary fetches them.
* `automations/*/automation.json` metadata `"url": "https://github.com/monoes/mono-agent"` and the
  `internal/crawlsite/fetch.go` user agent: the public repository is a correct landing page.
  `internal/httpapi/openapi.yaml` names `.../blob/master/LICENSE` as the licence URL; the public
  repository has `LICENSE` on `main`, so change it to `.../blob/main/LICENSE` or it 404s.
* `CHANGELOG.md` compare links, `docs/handoffs/*`, plans and code comments with `monoes/mono-agent#NNN`:
  issue and pull request numbers belong to `$SRC`.
* `chrome-extension/adapters/github.test.mjs` and its fixtures use `monoes/mono-agent` as sample
  GitHub pages for parsing tests; they never fetch it.
* `go.mod`, `wails-app/go.mod` and every import: `github.com/monoes/mono-agent` is only a module path
  (`wails-app` uses `replace ... => ..`). Do not change it.
* `AllowLegacyGitHubUpdates` (`internal/release/manifest.go`) is still `true`: its comment says to
  turn it off at "the repo-private release". With the releases repository public, the legacy path
  (GitHub API plus `SHA256SUMS.txt`) keeps working; turning it off is a separate security decision
  about older clients, not part of this split.
