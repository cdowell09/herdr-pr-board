# Fresh review fixture

Use this PR to test automatic review of a new PR identity.

Check these results:

- A selected view discovers the new open PR.
- The monitor starts one review when the PR is eligible.
- Review history records the outcome for this PR.
- Automatic comment publication follows the saved repository settings.
- Later scans do not repeat a completed review for the same revision.

Use a new commit to test another eligible revision.
