"""Sandbox for one eval task: a fresh scratch HOME, a clean environment for the
monoagentcli binary, a local fixture HTTP server and a small MCP stdio client.

Every CLI call goes through Sandbox.cli, which runs the binary with env -i style
isolation: HOME, XDG_* and TMPDIR point into the sandbox, BROWSER is /bin/true,
the webhook server binds a free loopback port (so it never collides with a
daemon the operator has running on 9321), and no agent markers (CLAUDECODE,
AI_AGENT, ...) leak in from the shell that started the harness.
"""

import functools
import http.server
import json
import os
import shutil
import socket
import subprocess
import tempfile
import threading

# A devaccount build reads this to move the account gate's enforcement date;
# a future date is the warn period, so commands work without a monoes.me
# sign-in. Release builds ignore it (and then refuse with exit 4).
DEV_ENFORCE_FROM = "2999-01-01T00:00:00Z"

# internal/cli/profilegate prints this when a profile-scoped command was not told
# which profile to act on; the harness records every call that did.
PROFILE_WARNING = "this command acts on a profile"

FIXTURE_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "fixtures")


def free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def scratch_root():
    """Where sandboxes live: $EVALS_SCRATCH, else ~/scratch when it exists
    (the main disk), else the system temp dir (CI)."""
    root = os.environ.get("EVALS_SCRATCH")
    if not root:
        cand = os.path.join(os.path.expanduser("~"), "scratch")
        root = cand if os.path.isdir(cand) else tempfile.gettempdir()
    os.makedirs(root, exist_ok=True)
    return root


class _QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


class FixtureServer:
    """Serves a directory on 127.0.0.1:<random port> from a daemon thread."""

    def __init__(self, directory):
        handler = functools.partial(_QuietHandler, directory=directory)
        self.httpd = http.server.ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.url = "http://127.0.0.1:%d" % self.httpd.server_address[1]
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)

    def __enter__(self):
        self.thread.start()
        return self

    def __exit__(self, *exc):
        self.httpd.shutdown()
        self.httpd.server_close()


class CLIResult:
    def __init__(self, args, code, stdout, stderr):
        self.args, self.code, self.stdout, self.stderr = args, code, stdout, stderr

    def json(self):
        """The JSON document on stdout (the CLI prints logs on stderr)."""
        try:
            return json.loads(self.stdout)
        except ValueError:
            return None


class Sandbox:
    """A throwaway HOME plus a work dir the agent may write files into."""

    def __init__(self, binary, root, name, fixture_url):
        self.binary = binary
        self.dir = tempfile.mkdtemp(prefix=name + "-", dir=root)
        self.home = os.path.join(self.dir, "home")
        self.work = os.path.join(self.dir, "work")  # agent's files (workflow JSON drafts)
        self.out = os.path.join(self.dir, "out")  # where tasks write their output files
        for d in (self.home, self.work, self.out, os.path.join(self.dir, "tmp")):
            os.makedirs(d, exist_ok=True)
        self.fixture_url = fixture_url
        self.webhook_port = free_port()
        # The named profile every scoped call acts on. Calls the harness makes
        # that omit it are recorded in unprofiled (the agent's own calls in
        # agent_unprofiled, which a model is allowed to get wrong).
        self.profile = "eval"
        self.unprofiled = []
        self.mcp_logs = []
        self.agent_unprofiled = []
        made = self.cli(["profile", "create", self.profile])
        if made.code != 0:
            raise RuntimeError("profile create %s: exit %d: %s" % (self.profile, made.code, made.stderr[-300:]))

    @property
    def p(self):
        """The --profile argument pair for a scoped call."""
        return ["--profile", self.profile]

    def env(self):
        h = self.home
        return {
            "PATH": os.environ.get("PATH", "/usr/bin:/bin"),
            "HOME": h,
            "USERPROFILE": h,
            "XDG_CONFIG_HOME": os.path.join(h, ".config"),
            "XDG_CACHE_HOME": os.path.join(h, ".cache"),
            "XDG_DATA_HOME": os.path.join(h, ".local", "share"),
            "XDG_STATE_HOME": os.path.join(h, ".local", "state"),
            "TMPDIR": os.path.join(self.dir, "tmp"),
            "BROWSER": "/bin/true",
            "MONOAGENT_DEV_ENFORCE_FROM": DEV_ENFORCE_FROM,
            "MONOAGENT_WEBHOOK_ADDR": "127.0.0.1:%d" % self.webhook_port,
        }

    def subst(self, text):
        """Fill the task/reference placeholders."""
        return (text.replace("{{FIXTURE_URL}}", self.fixture_url)
                .replace("{{FIXTURE_DIR}}", FIXTURE_DIR)
                .replace("{{OUT_DIR}}", self.out)
                .replace("{{WORK_DIR}}", self.work))

    def cli(self, args, stdin=None, timeout=120, agent=False):
        try:
            p = subprocess.run([self.binary] + list(args), input=stdin, capture_output=True,
                               text=True, env=self.env(), cwd=self.work, timeout=timeout)
            if PROFILE_WARNING in p.stderr:
                (self.agent_unprofiled if agent else self.unprofiled).append(list(args))
            return CLIResult(args, p.returncode, p.stdout, p.stderr)
        except subprocess.TimeoutExpired as e:
            return CLIResult(args, 124, e.stdout or "", (e.stderr or "") + "\n[timed out]")

    def mcp(self):
        err = os.path.join(self.dir, "mcp-%d.err" % len(self.mcp_logs))
        self.mcp_logs.append(err)
        return MCPClient([self.binary, "mcp", "--allow-mutations"] + self.p, self.env(), self.work, err)

    def profile_problems(self):
        """Calls the harness made without naming a profile (not the agent's)."""
        bad = ["cli %s" % " ".join(a) for a in self.unprofiled]
        for path in self.mcp_logs:
            try:
                with open(path, encoding="utf-8", errors="replace") as f:
                    if PROFILE_WARNING in f.read():
                        bad.append("mcp server (%s)" % os.path.basename(path))
            except OSError:
                pass
        return bad

    def cleanup(self):
        shutil.rmtree(self.dir, ignore_errors=True)


class MCPClient:
    """Minimal newline-delimited JSON-RPC 2.0 client for `monoagentcli mcp`."""

    def __init__(self, argv, env, cwd, stderr_path=os.devnull):
        self.proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=open(stderr_path, "w"), text=True, env=env, cwd=cwd)
        self.next_id = 0
        self.request("initialize", {"protocolVersion": "2024-11-05",
                                    "clientInfo": {"name": "monoagent-evals", "version": "1"}})
        self._send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def _send(self, msg):
        self.proc.stdin.write(json.dumps(msg) + "\n")
        self.proc.stdin.flush()

    def request(self, method, params=None):
        self.next_id += 1
        rid = self.next_id
        self._send({"jsonrpc": "2.0", "id": rid, "method": method, "params": params or {}})
        while True:
            line = self.proc.stdout.readline()
            if not line:
                raise RuntimeError("mcp server closed its stdout")
            try:
                msg = json.loads(line)
            except ValueError:
                continue
            if msg.get("id") == rid:
                if "error" in msg:
                    raise RuntimeError("mcp %s: %s" % (method, msg["error"]))
                return msg.get("result")

    def tools(self):
        return self.request("tools/list").get("tools", [])

    def call(self, name, arguments):
        """Returns (is_error, text) of a tools/call."""
        res = self.request("tools/call", {"name": name, "arguments": arguments or {}})
        text = "\n".join(c.get("text", "") for c in res.get("content", []) if c.get("type") == "text")
        return bool(res.get("isError")), text

    def close(self):
        try:
            self.proc.stdin.close()
            self.proc.wait(timeout=10)
        except Exception:
            self.proc.kill()
