# Replace manual review runs with command shortcuts

The board session must not start managed review runs.
A command shortcut replaces the `n` and `N` keys.
A command shortcut runs a user-configured command for the selected PR in a new Herdr pane.
PR Board starts the command in that pane.
PR Board does not prompt, drive, or track the command.
The command does not change the REVIEW column.
The user owns the command, its agent, and its output.
This keeps the boundary of [own the review appliance](0002-own-the-review-appliance.md).
PR Board does not drive the user's coding agent.
Automatic review runs stay managed.
The monitor, review memory, publication, and the `--review` command do not change.
A managed manual run requires checkouts, claims, and recorded findings for one key press.
Users prefer their own interactive review session for this work.
See [core user journeys](../journeys.md).
