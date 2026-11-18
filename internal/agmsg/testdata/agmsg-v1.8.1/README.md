# agmsg v1.8.1 fixtures

Captured at verified commit 6acb58a5d68a5eb589c0d8e1aa5e88bbefdabda2.

capture.sh <source-checkout> <commit-sha> installs into a throwaway HOME, joins a team, sends four messages and captures api.sh JSONL. It requires sqlite3 and Python 3. VERSION.txt and COMMIT.txt record provenance. Project paths are sanitized to /tmp/sample-project. The tests derive IDs and timestamps from this capture; the original v1.2.0 directory retains the historical ID-order regression.
