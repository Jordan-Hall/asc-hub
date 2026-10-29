#!/usr/bin/env python3
# A mock of the GitHub API v3 (enterprise layout, /api/v3), a Git LFS server and release asset downloads, for
# migration checks that must not touch the real GitHub: milestones, labels, releases with assets, LFS objects.
# usage: mock_github.py <port> <clone_url>   (clone_url is where the git data of mock/proj is served)
import hashlib, json, sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = int(sys.argv[1]); CLONE = sys.argv[2]
BASE = f"http://127.0.0.1:{PORT}"
LFS_BODY = b"large file content that lives in LFS\n" * 50
LFS_OID = hashlib.sha256(LFS_BODY).hexdigest()
ASSETS = {101: b"asset one bytes\n" * 10, 102: b"asset two bytes\n" * 20}
T = "2024-05-01T10:00:00Z"

def asset(i, name):
    return {"id": i, "name": name, "size": len(ASSETS[i]), "download_count": 3, "created_at": T, "updated_at": T,
            "url": f"{BASE}/api/v3/repos/mock/proj/releases/assets/{i}", "browser_download_url": f"{BASE}/dl/{i}",
            "content_type": "application/octet-stream", "state": "uploaded"}

def release(i, tag, name, assets):
    return {"id": i, "tag_name": tag, "target_commitish": "master", "name": name, "body": f"notes for {tag}", "draft": False,
            "prerelease": tag.startswith("v2"), "created_at": T, "published_at": T, "assets": assets,
            "author": {"id": 1, "login": "mockdev", "avatar_url": ""}, "html_url": f"{BASE}/mock/proj/releases/tag/{tag}"}

ROUTES = {
    "/api/v3/repos/mock/proj": {"id": 9, "name": "proj", "full_name": "mock/proj", "private": False, "description": "a mock project",
        "html_url": f"{BASE}/mock/proj", "clone_url": CLONE, "default_branch": "master", "homepage": "", "topics": ["mock", "asc"]},
    "/api/v3/repos/mock/proj/milestones": [
        {"number": 1, "title": "v1 goals", "description": "first", "state": "closed", "created_at": T, "updated_at": T, "closed_at": T, "due_on": T},
        {"number": 2, "title": "v2 goals", "description": "second", "state": "open", "created_at": T, "updated_at": T, "due_on": "2030-01-01T00:00:00Z"}],
    "/api/v3/repos/mock/proj/labels": [
        {"id": 1, "name": "bug", "color": "d73a4a", "description": "a bug"}, {"id": 2, "name": "docs", "color": "0075ca", "description": ""}],
    "/api/v3/repos/mock/proj/releases": [
        release(1, "v1.0", "First", [asset(101, "proj-1.0.tar.gz")]), release(2, "v2.0", "Second", [asset(102, "proj-2.0.tar.gz")])],
}
for k in ("issues", "pulls"):
    ROUTES[f"/api/v3/repos/mock/proj/{k}"] = []

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body, ctype="application/json", extra=None):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code); self.send_header("Content-Type", ctype); self.send_header("Content-Length", str(len(data)))
        for k, v in (extra or {}).items(): self.send_header(k, v)
        self.end_headers(); self.wfile.write(data)
    def do_GET(self):
        path = self.path.split("?")[0]
        if path in ROUTES: return self.send(200, ROUTES[path])
        if path.startswith("/api/v3/repos/mock/proj/releases/assets/"):
            return self.send(200, ASSETS[int(path.rsplit("/", 1)[1])], "application/octet-stream")
        if path == "/lfs/obj/" + LFS_OID: return self.send(200, LFS_BODY, "application/octet-stream")
        self.send(404, {"message": "Not Found"})
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0)); req = json.loads(self.rfile.read(n) or b"{}")
        if self.path.split("?")[0] == "/lfs/objects/batch":
            objs = []
            for o in req.get("objects", []):
                if o["oid"] == LFS_OID:
                    objs.append({"oid": o["oid"], "size": o["size"], "actions": {"download": {"href": f"{BASE}/lfs/obj/{o['oid']}"}}})
                else:
                    objs.append({"oid": o["oid"], "size": o["size"], "error": {"code": 404, "message": "not found"}})
            return self.send(200, {"transfer": "basic", "objects": objs}, "application/vnd.git-lfs+json")
        self.send(404, {"message": "Not Found"})

if __name__ == "__main__":
    print("lfs oid", LFS_OID, "size", len(LFS_BODY), flush=True)
    ThreadingHTTPServer(("127.0.0.1", PORT), H).serve_forever()
