# Local review memory

The `internal/reviewmemory` package owns review history and local claims.
It does not launch reviewers or publish findings.
Review commands and board controls follow in separate changes.

Supply `HERDR_PLUGIN_STATE_DIR` to `reviewmemory.Open`.
The package stores history and lock files in its `reviews` directory.
Processes must share this directory to coordinate reviews.
Use a local filesystem that supports advisory file locks and atomic replacement.
Coordination between separate computers is outside this contract.

## Review identity

A review identity contains the repository, PR number, head commit, and target branch.
Repository names use lowercase letters for comparison.
Head and comparison commits must contain 40 lowercase hexadecimal characters.
Title edits and reviewer changes do not change the identity.
A changed comparison commit alone does not change the identity.
Each attempt records its comparison commit and reviewer separately.
`History` returns attempts for one exact revision.
`PRHistory` returns attempts across all revisions of one PR.
Older findings do not complete a newer revision.

## Claims and outcomes

`Claim` reserves one revision and one installation concurrency slot atomically.
The default concurrency limit is one.
Callers must use the same configured limit across the installation.
An active claim prevents another claim, including explicit reruns.
Completed reviews prevent ordinary claims.
Failed, blocked, and abandoned attempts require an explicit rerun.
Set `Request.Rerun` to request that rerun.

`Finish` records an outcome for the owned attempt.
Completed outcomes require an explicit findings list and a summary.
An empty findings list is valid.
Findings require a severity, title, and description.
Supported severities are `P0`, `P1`, `P2`, and `P3`.
A positive line number requires a path.
Failed and blocked outcomes require a diagnostic message.
Callers must validate the reviewer result against the captured revision before completion.
Review completion does not grant publication permission or record publication.
Publication records belong to the separate publication workflow.

## Process ownership

Keep the claim open until the reviewer stops.
Use `LockFile` with `exec.Cmd.ExtraFiles` when a reviewer can outlive its parent.
Close the returned descriptor after the child starts.
The child must keep its inherited descriptor open throughout execution.
`Close` releases only the calling process's descriptor.
The next history operation marks an unlocked running attempt as abandoned.
A closed claim cannot complete an attempt or overwrite a replacement attempt.

The package writes history through an atomic file replacement.
Malformed history stops claims and updates.
The package does not replace malformed history with an empty history.
Preserve malformed files before manual repair.
