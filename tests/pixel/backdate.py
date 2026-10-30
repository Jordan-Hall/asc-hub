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
if '--logins' in sys.argv:
    # the harness signs people in after the seed, which stamps them with the real time; the browser clock is the anchor
    cur.execute('UPDATE user SET last_login_unix=?', (anchor - 5 * M,))
    con.commit()
    print('logins backdated')
    sys.exit(0)
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
# invitations that have not been answered (the People page): a person who has an account and an address that has none. They are
# put in here because the seed adds people to teams directly, and an invitation is what the hub makes only once it is switched on.
cur.execute("SELECT id FROM user WHERE lower_name='acme' AND type=1")
org = cur.fetchone()
cur.execute("SELECT id FROM user WHERE lower_name='maya'")
maya = cur.fetchone()
cur.execute("SELECT id FROM user WHERE lower_name='ines'")
ines = cur.fetchone()
cur.execute("SELECT id, lower_name FROM team WHERE org_id=?", (org[0],)) if org else None
teams = {name: tid for tid, name in cur.fetchall()} if org else {}
# the organization has proved its domain (the Verified chip of its head); the proof is a DNS record the harness cannot publish
if org:
    cur.execute('INSERT OR IGNORE INTO asc_org_domain (org_id, domain, token, verified_unix, checked_unix) VALUES (?,?,?,?,?)',
                (org[0], 'acme.example', 'pixel-harness-domain-token', anchor - 30 * 1440 * M, anchor - 1440 * M))
if org and maya and ines and 'payments' in teams and 'reviewers' in teams:
    for token, team, email, invited, age in [('pixel-invite-ines', 'payments', '', ines[0], 2 * 1440), ('pixel-invite-sam', 'reviewers', 'sam@northwind.example', None, 2 * 1440)]:
        t = anchor - age * M
        cur.execute('INSERT INTO team_invite (token, inviter_id, org_id, team_id, email, invited_id, created_unix, updated_unix) VALUES (?,?,?,?,?,?,?,?)',
                    (token, maya[0], org[0], teams[team], email, invited, t, t))
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
