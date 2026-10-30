"""Moves the creation times of the seeded rows to fixed offsets before the anchor, so the relative times the hub
prints ("12 minutes ago", "Yesterday") match the design boards on every run.
usage: python3 backdate.py <hub.db> <anchor-unix-seconds> [<asc.db>]      (run while the hub and asc serve are stopped)
With asc.db (PIXEL_ASC=serve) the times asc keeps move too: the releases to their ages, every change and review to just before the anchor."""
import sqlite3, sys

db, anchor = sys.argv[1], int(sys.argv[2])
M = 60
# minutes before the anchor, by title (stacks and issues) or tag (releases) or repository name
ISSUES = {
    'Retry every outbound payment call with jittered backoff': 12,
    'Raise tokio to 1.41': 3, 'Nightly ledger export to CSV': 1440,
    # the changes of the stacks made with the asc client (seed-asc.mjs: the age of the newest, four minutes between changes)
    'Wire the retry policy into the client': 16, 'Docs and metrics for the retry policy': 12,
    'Refund-specific retry policy': 184, 'Replay test for refund retries': 180, 'Idempotency keys on refunds': 60,
    'Retries stampede the provider after an outage': 120, 'Refund endpoint accepts the same key with a different amount': 300,
    'Document the failover order for card providers': 1500, 'Export ledger as CSV from the dashboard': 2900,
    'Webhook retries should back off per receiver': 5800, 'Timeout in sandbox gateway test is flaky': 8700,
}
RELEASES = {'v2.18.0': 8640, 'v2.19.0-rc1': 1440, 'v2.18.0-rc.1': 18720, 'v2.17.2': 40320, 'v2.17.0': 74880}
REPOS = {'payments': 12, 'payments-core': 12, 'web-app': 41, 'infra': 120, 'ledger': 180, 'design-system': 1440, 'legacy-batch': 216000}

con = sqlite3.connect(db)
cur = con.cursor()
for title, minutes in ISSUES.items():
    t = anchor - minutes * M
    cur.execute('UPDATE issue SET created_unix=?, updated_unix=? WHERE name=?', (t, t, title))
    cur.execute('SELECT id FROM issue WHERE name=?', (title,))
    row = cur.fetchone()
    if row:
        cur.execute('SELECT id FROM comment WHERE issue_id=? ORDER BY id', (row[0],))
        for n, (cid,) in enumerate(cur.fetchall(), 1):
            ct = min(anchor - M, t + n * 7 * M)
            cur.execute('UPDATE comment SET created_unix=?, updated_unix=? WHERE id=?', (ct, ct, cid))
for tag, minutes in RELEASES.items():
    t = anchor - minutes * M
    cur.execute('UPDATE release SET created_unix=? WHERE tag_name=?', (t, tag))
for name, minutes in REPOS.items():
    t = anchor - minutes * M
    cur.execute('UPDATE repository SET updated_unix=?, created_unix=? WHERE lower_name=?', (t, anchor - 90 * 1440 * M, name))
cur.execute('UPDATE user SET created_unix=?, updated_unix=?, last_login_unix=?', (anchor - 500 * 1440 * M, anchor - M, anchor - 5 * M))
con.commit()
if len(sys.argv) > 3:
    ac = sqlite3.connect(sys.argv[3])
    for tag, minutes in RELEASES.items():
        t = anchor - minutes * M
        ac.execute('UPDATE releases SET created=?, published=CASE WHEN published > 0 THEN ? ELSE 0 END WHERE tag=?', (t, t, tag))
    # changes and reviews keep their gaps and end 5 minutes before the anchor
    (newest,) = ac.execute('SELECT MAX(at) FROM change_versions').fetchone()
    if newest:
        shift = anchor - 5 * M - newest
        ac.execute('UPDATE change_versions SET at = at + ?', (shift,))
        ac.execute('UPDATE review_state SET updated = updated + ?', (shift,))
        ac.execute('UPDATE review_approvals SET at = at + ?', (shift,))
        ac.execute('UPDATE review_threads SET created = created + ?, updated = updated + ?', (shift, shift))
        ac.execute('UPDATE review_comments SET created = created + ?', (shift,))
    ac.commit()
print('backdated')
