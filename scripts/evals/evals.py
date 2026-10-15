#!/usr/bin/env python3
"""Capability eval suite: how well can an external agent author working
monoagentcli workflows through the CLI and the MCP server? See README.md.

  evals.py --bin <monoagentcli> --reference               score the reference solutions (no model)
  evals.py --bin <monoagentcli> --model <id>:free [...]   run the agent per task, then score
"""

import argparse
import concurrent.futures
import datetime
import glob
import json
import os
import re
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from evallib import harness, report, scorer  # noqa: E402

HERE = os.path.dirname(os.path.abspath(__file__))


def load_tasks(only):
    tasks = []
    for path in sorted(glob.glob(os.path.join(HERE, "tasks", "*.json"))):
        with open(path, encoding="utf-8") as f:
            t = json.load(f)
        if t["id"] != os.path.splitext(os.path.basename(path))[0]:
            sys.exit("task file %s: id %r must match the file name" % (path, t["id"]))
        if not only or t["id"] in only:
            tasks.append(t)
    if only and len(tasks) != len(only):
        sys.exit("unknown task id(s): %s" % sorted(set(only) - {t["id"] for t in tasks}))
    return tasks


def modes_for(task, wanted):
    m = task.get("mode", "both")
    have = ["cli", "mcp"] if m == "both" else [m]
    return [x for x in wanted if x in have]


def probe_gate(binary, root, fixture_url):
    sb = harness.Sandbox(binary, root, "probe", fixture_url)
    try:
        r = sb.cli(sb.p + ["--json", "workflow", "list"])
        if r.code == 4:
            sys.exit("the binary refuses with the account gate (exit 4): build it with -tags devaccount, "
                     "e.g. go build -tags devaccount -o <path> ./cmd/monoagentcli")
        if r.code != 0:
            sys.exit("probe `workflow list` failed (exit %d): %s" % (r.code, r.stderr[-500:]))
    finally:
        sb.cleanup()


# Mutants keep the scorer honest: a reference saved under another task's
# workflow name must fail that task's checks.
MUTANTS = [("sort-limit", "aggregate-sum"), ("if-routing", "switch-routing"),
           ("schedule-daily", "schedule-only"), ("dedupe", "http-filter-csv")]


def profile_check(sb):
    """A failing check when the harness made a scoped call without --profile.
    The agent's own calls are not judged here: a model may forget the flag."""
    bad = sb.profile_problems()
    if not bad:
        return []
    return [{"check": "profile guard", "type": "profile_guard", "pass": False,
             "detail": "call(s) named no profile: " + "; ".join(bad)[:300]}]


def run_reference(task, args, root, fixture_url, ref_id=None):
    ref_id = ref_id or task["id"]
    sb = harness.Sandbox(args.bin, root, task["id"] + ("" if ref_id == task["id"] else "-mutant"), fixture_url)
    start = time.time()
    try:
        ref = os.path.join(HERE, "reference", ref_id + ".json")
        with open(ref, encoding="utf-8") as f:
            doc = json.loads(sb.subst(f.read()))
        doc["name"] = task.get("workflow_name") or "eval-" + task["id"]
        doc = json.dumps(doc)
        path = os.path.join(sb.work, "reference.json")
        with open(path, "w", encoding="utf-8") as f:
            f.write(doc)
        imp = sb.cli(sb.p + ["--json", "workflow", "import", "--file", path])
        checks = scorer.Scorer(sb, task).score() + profile_check(sb)
        if imp.code != 0:
            checks.insert(0, {"check": "import", "type": "import", "pass": False,
                              "detail": "exit %d: %s" % (imp.code, imp.stderr.strip()[-300:])})
        return {"task": task["id"], "mode": "reference" if ref_id == task["id"] else "mutant:" + ref_id,
                "checks": checks, "steps": 0,
                "seconds": round(time.time() - start, 1)}
    finally:
        if not args.keep:
            sb.cleanup()


def run_live(task, mode, args, root, fixture_url, agent, transcripts):
    sb = harness.Sandbox(args.bin, root, "%s-%s" % (task["id"], mode), fixture_url)
    try:
        run = agent.run(sb, mode, sb.subst(task["prompt"]))
        checks = scorer.Scorer(sb, task).score() + profile_check(sb)
        tpath = os.path.join(transcripts, "%s-%s.json" % (task["id"], mode))
        with open(tpath, "w", encoding="utf-8") as f:
            json.dump(run["transcript"], f, indent=1)
        return {"task": task["id"], "mode": mode, "checks": checks, "steps": run["steps"],
                "seconds": run["seconds"], "finished": run["finished"], "agent_error": run["error"],
                "transcript": tpath}
    finally:
        if not args.keep:
            sb.cleanup()


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--bin", required=True, help="monoagentcli binary built with -tags devaccount")
    ap.add_argument("--reference", "--dry-check", dest="reference", action="store_true",
                    help="score reference/<task>.json instead of running a model")
    ap.add_argument("--model", help="OpenRouter model id (a :free one); key from $OPENROUTER_API_KEY")
    ap.add_argument("--tasks", default="", help="comma-separated task ids (default: all)")
    ap.add_argument("--modes", default="cli", help="live modes to run: cli, mcp or cli,mcp")
    ap.add_argument("--max-steps", type=int, default=30)
    ap.add_argument("--max-seconds", type=int, default=600)
    ap.add_argument("--jobs", type=int, default=4, help="parallel tasks in reference mode")
    ap.add_argument("--results-dir", default=os.path.join(HERE, "results"))
    ap.add_argument("--keep", action="store_true", help="keep the sandboxes")
    args = ap.parse_args()
    args.bin = os.path.abspath(args.bin)
    if not os.access(args.bin, os.X_OK):
        sys.exit("--bin %s is not an executable" % args.bin)
    if not args.reference and not args.model:
        sys.exit("give --reference or --model")

    tasks = load_tasks([t for t in args.tasks.split(",") if t])
    stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    root = os.path.join(harness.scratch_root(), "monoagent-evals-" + stamp)
    os.makedirs(root, exist_ok=True)
    started = time.time()
    with harness.FixtureServer(harness.FIXTURE_DIR) as fx:
        probe_gate(args.bin, root, fx.url)
        if args.reference:
            with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, args.jobs)) as pool:
                results = list(pool.map(lambda t: run_reference(t, args, root, fx.url), tasks))
                by_id = {t["id"]: t for t in tasks}
                pairs = [(r, t) for r, t in MUTANTS if t in by_id]
                mutants = list(pool.map(lambda p: run_reference(by_id[p[1]], args, root, fx.url, p[0]), pairs))
        else:
            from evallib.agent import Agent
            key = os.environ.get("OPENROUTER_API_KEY")
            if not key:
                sys.exit("set OPENROUTER_API_KEY for a live run")
            if not args.model.endswith(":free"):
                sys.exit("live runs use a :free OpenRouter model only")
            agent = Agent(args.model, key, args.max_steps, args.max_seconds)
            transcripts = os.path.join(root, "transcripts")
            os.makedirs(transcripts, exist_ok=True)
            results = []
            for t in tasks:
                for mode in modes_for(t, args.modes.split(",")):
                    print("== %s (%s)" % (t["id"], mode), flush=True)
                    results.append(run_live(t, mode, args, root, fx.url, agent, transcripts))

    label = "reference" if args.reference else args.model
    doc = {"v": 1, "label": label, "binary": args.bin, "started": stamp,
           "seconds": round(time.time() - started, 1), "results": results}
    md = report.markdown(doc)
    print(md)
    if not args.reference:
        os.makedirs(args.results_dir, exist_ok=True)
        slug = re.sub(r"[^A-Za-z0-9._-]+", "_", label)
        base = os.path.join(args.results_dir, "%s-%s" % (datetime.date.today().isoformat(), slug))
        with open(base + ".json", "w", encoding="utf-8") as f:
            json.dump(doc, f, indent=1)
        with open(base + ".md", "w", encoding="utf-8") as f:
            f.write(md)
        print("wrote %s.json and %s.md" % (base, base))
    else:
        if not args.keep:
            try:
                os.rmdir(root)
            except OSError:
                pass
        failed = [r["task"] for r in results if not all(c["pass"] for c in r["checks"])]
        if failed:
            print("reference mode: FAILED for %s" % ", ".join(failed))
            sys.exit(1)
        lax = [r["task"] + " (" + r["mode"] + ")" for r in mutants if all(c["pass"] for c in r["checks"])]
        for r in mutants:
            print("mutant %s scored as %s: %d/%d checks pass (must not be all)" % (
                r["mode"][len("mutant:"):], r["task"], sum(c["pass"] for c in r["checks"]), len(r["checks"])))
        if lax:
            print("reference mode: the scorer accepted a wrong workflow for %s" % ", ".join(lax))
            sys.exit(1)
        print("reference mode: all %d references pass, all %d mutants are rejected" % (len(results), len(mutants)))


if __name__ == "__main__":
    main()
