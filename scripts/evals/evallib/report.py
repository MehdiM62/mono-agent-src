"""Markdown summary of a results document."""


def markdown(doc):
    rows = doc["results"]
    lines = ["# monoagentcli capability evals: %s" % doc["label"], "",
             "Started %s, %.1fs total, binary `%s`." % (doc["started"], doc["seconds"], doc["binary"]), "",
             "| task | mode | result | checks | steps | seconds |",
             "|---|---|---|---|---|---|"]
    passed_tasks = passed_checks = total_checks = 0
    for r in rows:
        ok = sum(1 for c in r["checks"] if c["pass"])
        total = len(r["checks"])
        passed_checks += ok
        total_checks += total
        full = ok == total
        passed_tasks += full
        cells = " ".join("%s:%s" % (c["check"], "PASS" if c["pass"] else "FAIL") for c in r["checks"])
        lines.append("| %s | %s | %s %d/%d | %s | %s | %s |" % (
            r["task"], r["mode"], "PASS" if full else "FAIL", ok, total, cells, r.get("steps", 0),
            r.get("seconds", 0)))
    lines += ["", "Tasks fully passed: %d/%d. Checks passed: %d/%d." % (
        passed_tasks, len(rows), passed_checks, total_checks), ""]
    fails = [(r, c) for r in rows for c in r["checks"] if not c["pass"]]
    if fails:
        lines += ["## Failed checks", ""]
        for r, c in fails:
            lines.append("- %s (%s) %s: %s" % (r["task"], r["mode"], c["check"], c["detail"].replace("\n", " ")))
        lines.append("")
    errs = [r for r in rows if r.get("agent_error")]
    if errs:
        lines += ["## Agent errors", ""] + ["- %s (%s): %s" % (r["task"], r["mode"], r["agent_error"]) for r in errs]
        lines.append("")
    return "\n".join(lines)
