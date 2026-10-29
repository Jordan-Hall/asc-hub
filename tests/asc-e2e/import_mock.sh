#!/bin/bash
# migrates the mock GitHub project (releases with assets, milestones, labels, LFS) into the hub on :3100 as Jordan
# and prints what arrived. Needs the dumb http server on 8099 (python3 -m http.server 8099 --bind 127.0.0.1, in /tmp).
cd /tmp
D=$(dirname "$(readlink -f "$0")"); A="Jordan:Passw0rd!Passw0rd"; H=http://127.0.0.1:3100; N=${1:-rel$RANDOM}
pkill -f "mock_github.py 8098" 2>/dev/null; sleep 0.5
nohup python3 $D/mock_github.py 8098 http://127.0.0.1:8099/src-rel.git > /tmp/mock.log 2>&1 &
sleep 1; read -r _ OID _ SIZE < <(head -1 /tmp/mock.log | sed 's/lfs oid \(\S*\) size \(\S*\)/x \1 y \2/')
bash $D/make_source.sh "$OID" "$SIZE"
curl -s -u "$A" -H "Content-Type: application/json" -d "{\"clone_addr\":\"http://127.0.0.1:8098/mock/proj\",\"service\":\"github\",\"repo_name\":\"$N\",\"repo_owner\":\"Jordan\",\"milestones\":true,\"labels\":true,\"releases\":true,\"lfs\":true,\"lfs_endpoint\":\"http://127.0.0.1:8098/lfs\"}" $H/api/v1/repos/migrate -o /tmp/mock-mig.json -w "migrate http %{http_code}\n"
head -c 300 /tmp/mock-mig.json | grep -o '"message":"[^"]*"'
sleep 3
for p in milestones labels releases; do echo "$p: $(curl -s -u "$A" "$H/api/v1/repos/Jordan/$N/$p?state=all" | python3 -c 'import sys,json; print([ (x.get("title") or x.get("name") or x.get("tag_name")) for x in json.load(sys.stdin)])')"; done
curl -s -u "$A" "$H/api/v1/repos/Jordan/$N/releases" | python3 -c 'import sys,json; [print(" release", r["tag_name"], "assets", [(a["name"], a["size"]) for a in r["assets"]]) for r in json.load(sys.stdin)]'
echo "lfs file via media: $(curl -s -u "$A" "$H/Jordan/$N/media/branch/master/big.bin" | wc -c) bytes (want $SIZE)"
echo "name: $N"
