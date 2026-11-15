# agmsg v1.7.3 fixtures

Captured at verified commit 9fea22e477aece18bd80ab4346b17d1c986731e1.

capture.sh <source-checkout> <commit-sha> installs into a throwaway HOME, joins a team, sends four messages and captures api.sh JSONL. It requires sqlite3 and Python 3. VERSION.txt and COMMIT.txt record provenance. Project paths are sanitized to /tmp/sample-project. The tests derive IDs and timestamps from this capture; the original v1.2.0 directory retains the historical ID-order regression.
