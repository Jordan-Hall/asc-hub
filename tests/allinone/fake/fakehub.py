#!/usr/bin/env python3
"""A small fake of the parts of ASC Hub that scripts/smoke.sh talks to.

  fakehub.py PORT STATE_DIR USER TOKEN

State lives under STATE_DIR: repos/<user>/<name>.git (real bare repositories served through
`git http-backend`), keys (one "id public-key" line per key), requests.log (one line per request).
Behaviour switches, from the environment:

  FAKE_NO_WELLKNOWN=1     /.well-known/asc answers 404
  FAKE_WELLKNOWN_HTTP=1   /.well-known/asc announces a plain http address on another host
  FAKE_REJECT_PUSH=1      git-receive-pack answers 403
  FAKE_WRONG_OWNER=1      the answer to creating a repository names another owner
  FAKE_SIGNIN_REQUIRED=1  the repository page redirects to the sign-in page
  FAKE_DELETE_FAILS=1     deleting a repository answers 500
  FAKE_SSH_HOST=name      host name in ssh_url (default 127.0.0.1)
"""
import base64
import json
import os
import re
import signal
import subprocess
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

PORT = int(sys.argv[1])
STATE = sys.argv[2]
USER = sys.argv[3]
TOKEN = sys.argv[4]
REPOS = os.path.join(STATE, "repos")
KEYS = os.path.join(STATE, "keys")
LOG = os.path.join(STATE, "requests.log")
os.makedirs(REPOS, exist_ok=True)
open(KEYS, "a").close()
signal.signal(signal.SIGTERM, lambda *_: sys.exit(0))

NAME_RE = re.compile(r"^[A-Za-z0-9._-]{1,100}$")


def flag(name):
    return os.environ.get(name) == "1"


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_args):
        pass

    # --- helpers ---

    def authed(self):
        h = self.headers.get("Authorization", "")
        if h == "token " + TOKEN:
            return True
        if h.startswith("Basic "):
            try:
                user, _, pw = base64.b64decode(h[6:]).decode().partition(":")
            except Exception:
                return False
            return user == USER and pw == TOKEN
        return False

    def send(self, code, body=b"", ctype="application/json", extra=None):
        if isinstance(body, str):
            body = body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        for k, v in (extra or {}).items():
            self.send_header(k, v)
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(n) if n else b""

    def record(self):
        with open(LOG, "a") as f:
            f.write("%s %s\n" % (self.command, self.path.split("?")[0]))

    def repo_dir(self, owner, name):
        if owner != USER or not NAME_RE.match(name):
            return None
        return os.path.join(REPOS, owner, name + ".git")

    def git(self, repo, *args):
        return subprocess.run(["git", "-C", repo, *args], capture_output=True)

    # --- routing ---

    def do_GET(self):
        self.route()

    def do_POST(self):
        self.route()

    def do_DELETE(self):
        self.route()

    def route(self):
        self.record()
        parts = urlsplit(self.path)
        path = parts.path
        if path == "/api/healthz":
            return self.send(200, '{"status":"pass"}')
        if path == "/.well-known/asc":
            if flag("FAKE_NO_WELLKNOWN"):
                return self.send(404, "")
            server = "http://evil.example.org:7890" if flag("FAKE_WELLKNOWN_HTTP") else "http://127.0.0.1:%d" % PORT
            return self.send(200, json.dumps({"server": server, "repos": "*"}, separators=(",", ":")))
        if path == "/user/login":
            return self.send(200, '<html><body><form method="post"><input name="user_name"></form></body></html>', "text/html")
        if path == "/api/v1/user":
            if not self.authed():
                return self.send(401, '{"message":"token is required"}')
            return self.send(200, json.dumps({"id": 1, "login": USER}, separators=(",", ":")))
        if path.endswith(".git/info/refs") or "/git-upload-pack" in path or "/git-receive-pack" in path:
            return self.git_http(path, parts.query)
        m = re.match(r"^/api/v1/repos/([^/]+)/([^/]+)(/.*)?$", path)
        if m:
            return self.repo_api(m.group(1), m.group(2), m.group(3) or "", parts.query)
        if path == "/api/v1/user/repos" and self.command == "POST":
            return self.create_repo()
        if path == "/api/v1/user/keys" and self.command == "POST":
            return self.add_key()
        m = re.match(r"^/api/v1/user/keys/(\d+)$", path)
        if m and self.command == "DELETE":
            return self.delete_key(m.group(1))
        m = re.match(r"^/([^/]+)/([^/]+)$", path)
        if m and self.command == "GET":
            return self.page(m.group(1), m.group(2))
        return self.send(404, '{"message":"not found"}')

    # --- API ---

    def create_repo(self):
        if not self.authed():
            return self.send(401, "{}")
        try:
            req = json.loads(self.body())
        except ValueError:
            return self.send(422, "{}")
        name = req.get("name", "")
        d = self.repo_dir(USER, name)
        if d is None or os.path.exists(d):
            return self.send(422, '{"message":"bad name"}')
        os.makedirs(d)
        subprocess.run(["git", "init", "-q", "--bare", "--initial-branch=main", d], check=True)
        self.git(d, "config", "http.receivepack", "true")
        owner = "mallory" if flag("FAKE_WRONG_OWNER") else USER
        base = "http://127.0.0.1:%d" % PORT
        out = {
            "id": 42,
            "name": name,
            "default_branch": "main",
            "clone_url": "%s/%s/%s.git" % (base, owner, name),
            "ssh_url": "ssh://git@%s:2222/%s/%s.git" % (os.environ.get("FAKE_SSH_HOST", "127.0.0.1"), USER, name),
            "owner": {"login": owner, "full_name": ""},
        }
        return self.send(201, json.dumps(out, separators=(",", ":")))

    def repo_api(self, owner, name, rest, query):
        if not self.authed():
            return self.send(401, "{}")
        d = self.repo_dir(owner, name)
        if d is None or not os.path.isdir(d):
            return self.send(404, '{"message":"not found"}')
        if self.command == "DELETE" and rest == "":
            if flag("FAKE_DELETE_FAILS"):
                return self.send(500, "{}")
            subprocess.run(["rm", "-rf", d], check=True)
            return self.send(204, "")
        if self.command == "GET" and rest == "":
            return self.send(200, json.dumps({
                "name": name,
                "default_branch": "main",
                "clone_url": "http://127.0.0.1:%d/%s/%s.git" % (PORT, owner, name),
                "ssh_url": "ssh://git@%s:2222/%s/%s.git" % (os.environ.get("FAKE_SSH_HOST", "127.0.0.1"), owner, name),
            }, separators=(",", ":")))
        m = re.match(r"^/branches/([A-Za-z0-9._/-]+)$", rest)
        if m and self.command == "GET":
            r = self.git(d, "rev-parse", "--verify", "-q", "refs/heads/" + m.group(1))
            return self.send(200 if r.returncode == 0 else 404, '{"name":"%s"}' % m.group(1))
        m = re.match(r"^/raw/([A-Za-z0-9._/-]+)$", rest)
        if m and self.command == "GET":
            ref = parse_qs(query).get("ref", ["main"])[0]
            if not re.match(r"^[A-Za-z0-9._/-]+$", ref):
                return self.send(400, "{}")
            r = self.git(d, "show", "refs/heads/%s:%s" % (ref, m.group(1)))
            if r.returncode != 0:
                return self.send(404, "{}")
            return self.send(200, r.stdout, "text/plain")
        return self.send(404, "{}")

    def add_key(self):
        if not self.authed():
            return self.send(401, "{}")
        req = json.loads(self.body())
        with open(KEYS) as f:
            n = len(f.readlines()) + 1
        with open(KEYS, "a") as f:
            f.write("%d %s\n" % (n, " ".join(req["key"].split()[:2])))
        return self.send(201, json.dumps({"id": n, "title": req["title"], "key": req["key"]}, separators=(",", ":")))

    def delete_key(self, kid):
        if not self.authed():
            return self.send(401, "{}")
        with open(KEYS) as f:
            lines = [l for l in f.readlines() if not l.startswith(kid + " ")]
        with open(KEYS, "w") as f:
            f.writelines(lines)
        return self.send(204, "")

    def page(self, owner, name):
        if flag("FAKE_SIGNIN_REQUIRED"):
            return self.send(303, "", "text/html", {"Location": "/user/login"})
        d = self.repo_dir(owner, name)
        if d is None or not os.path.isdir(d):
            return self.send(404, "not found", "text/html")
        return self.send(200, "<html><body><h1>%s / %s</h1></body></html>" % (owner, name), "text/html")

    # --- git over http, through git http-backend ---

    def git_http(self, path, query):
        data = self.body() if self.command == "POST" else b""
        m = re.match(r"^/([^/]+)/([^/]+\.git)(/.*)$", path)
        if not m:
            return self.send(404, "")
        if not self.authed():
            return self.send(401, "", "text/plain", {"WWW-Authenticate": 'Basic realm="fake"'})
        if flag("FAKE_REJECT_PUSH") and "git-receive-pack" in (path + query):
            return self.send(403, "push refused")
        env = {
            "PATH": os.environ["PATH"],
            "GIT_PROJECT_ROOT": REPOS,
            "GIT_HTTP_EXPORT_ALL": "1",
            "PATH_INFO": path,
            "QUERY_STRING": query,
            "REQUEST_METHOD": self.command,
            "CONTENT_TYPE": self.headers.get("Content-Type", ""),
            "CONTENT_LENGTH": str(len(data)),
            "REMOTE_USER": USER,
            "REMOTE_ADDR": "127.0.0.1",
            "HTTP_GIT_PROTOCOL": self.headers.get("Git-Protocol", ""),
            "HTTP_CONTENT_ENCODING": self.headers.get("Content-Encoding", ""),
        }
        r = subprocess.run(["git", "http-backend"], input=data, env=env, capture_output=True)
        head, _, body = r.stdout.partition(b"\r\n\r\n")
        status = 200
        headers = {}
        for line in head.decode().split("\r\n"):
            k, _, v = line.partition(": ")
            if k.lower() == "status":
                status = int(v.split()[0])
            elif k:
                headers[k] = v
        ctype = headers.pop("Content-Type", "application/octet-stream")
        headers.pop("Content-Length", None)
        return self.send(status, body, ctype, headers)


ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
