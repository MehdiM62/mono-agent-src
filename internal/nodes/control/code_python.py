# Bootstrap for core.code's Python language. CodeNode runs it with the
# system python3: the task arrives as JSON on stdin and the result leaves
# as one JSON line, the last on stdout. Code reaches the n8n Python names:
# _items / _item (n8n's task runner), _input, _json and _('Node') (its
# earlier Pyodide mode, whose items also read as attributes: item.json.x).
import json
import sys
import traceback

FILENAME = "<code>"
_code_lines = []


class _Obj(dict):
    """A dict whose keys also read and write as attributes."""

    __slots__ = ()

    def __getattr__(self, key):
        try:
            return self[key]
        except KeyError:
            raise AttributeError(key) from None

    def __setattr__(self, key, value):
        self[key] = value

    def __delattr__(self, key):
        try:
            del self[key]
        except KeyError:
            raise AttributeError(key) from None

    def to_py(self):
        return json.loads(json.dumps(self, default=str))


class _Items:
    """_input and _('Node'): all(), first(), last() and item."""

    def __init__(self, items, item=None):
        self._items = items
        self.item = item if item is not None else (items[0] if items else None)

    def all(self):
        return self._items

    def first(self):
        return self._items[0] if self._items else None

    def last(self):
        return self._items[-1] if self._items else None


class _UserError(Exception):
    pass


def _item_json(value, index):
    # A returned {"json": {...}} (with optional binary/pairedItem) is an
    # n8n item; any other dict is the item's JSON itself.
    if not isinstance(value, dict):
        raise _UserError(
            "code must return a dictionary or a list of dictionaries, got %s at index %d"
            % (type(value).__name__, index)
        )
    inner = value.get("json")
    if isinstance(inner, dict) and set(value) <= {"json", "binary", "pairedItem"}:
        return inner
    return value


def _run(code, env):
    wrapped = "def _user_function():\n"
    for line in code.splitlines() or ["pass"]:
        wrapped += "    " + line + "\n"
    wrapped += "    pass\n"
    compiled = compile(wrapped, FILENAME, "exec")
    scope = dict(env)
    scope["__name__"] = "__main__"
    exec(compiled, scope)
    return scope["_user_function"]()


def _globals(task):
    nodes = {name: [_Obj(json=j) for j in items] for name, items in task.get("nodes", {}).items()}

    def node(name):
        if name not in nodes:
            raise KeyError(
                "_(%r): no output from a node with that name (it has not run, or does not exist)" % name
            )
        return _Items(nodes[name])

    return {
        "_": node,
        "_workflow": task.get("workflow"),
        "_execution": task.get("execution"),
        "_runIndex": task.get("runIndex", 0),
        "_query": None,
    }


def main():
    task = json.loads(sys.stdin.buffer.read().decode("utf-8"), object_hook=_Obj)
    _code_lines.extend(task["code"].splitlines())
    out = sys.stdout
    # print() in the code goes to stderr, keeping stdout for the result.
    sys.stdout = sys.stderr
    items = [_Obj(json=j) for j in task["items"]]
    base = _globals(task)
    result = []
    if task.get("mode") == "runOnceForEachItem":
        for index, item in enumerate(items):
            env = dict(base, _item=item, _input=_Items(items, item), _json=item["json"])
            value = _run(task["code"], env)
            if value is None:
                continue
            result.append(_item_json(value, index))
    else:
        env = dict(base, _items=items, _input=_Items(items), _json=items[0]["json"] if items else _Obj())
        value = _run(task["code"], env)
        if value is None:
            value = []
        elif isinstance(value, dict):
            value = [value]
        elif not isinstance(value, (list, tuple)):
            raise _UserError(
                "code must return a dictionary or a list of dictionaries, got %s" % type(value).__name__
            )
        result = [_item_json(v, i) for i, v in enumerate(value)]
    line = json.dumps({"items": result}, default=str, allow_nan=False, ensure_ascii=False)
    out.buffer.write(b"\n" + line.encode("utf-8") + b"\n")
    out.flush()


def _report(exc):
    # Only the frames of the user's code, with their own line numbers
    # (the wrapper adds one line on top).
    def at(lineno):
        text = _code_lines[lineno - 1].strip() if 0 < lineno <= len(_code_lines) else ""
        return "line %d: %s" % (lineno, text)

    lines = []
    if isinstance(exc, SyntaxError) and exc.filename == FILENAME:
        lines.append(at((exc.lineno or 1) - 1))
    else:
        for frame in traceback.extract_tb(exc.__traceback__):
            if frame.filename == FILENAME:
                lines.append(at(frame.lineno - 1))
    message = str(exc) if isinstance(exc, _UserError) else "%s: %s" % (type(exc).__name__, exc)
    if isinstance(exc, SyntaxError):
        message = "SyntaxError: %s" % exc.msg
    return {"error": {"message": message, "traceback": lines}}


if __name__ == "__main__":
    try:
        main()
    except BaseException as exc:  # SystemExit and KeyboardInterrupt too: report, never hang
        line = json.dumps(_report(exc), default=str, ensure_ascii=False)
        sys.__stdout__.buffer.write(b"\n" + line.encode("utf-8") + b"\n")
        sys.__stdout__.flush()
        sys.exit(1)
