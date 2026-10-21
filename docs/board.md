# Board controls and status

The plugin supplies these default views:

- **Opened by me** shows each open PR that your GitHub account created.
- **Review requested** shows each open PR that requests your review.
- **All open** shows each open PR in the configured scopes.

You can add, remove, rename, or move views in `config.toml`.
Each tab shows the view title and its PR count, for example `Opened by me (4)`.
A dim dot separates the tabs. The number keys `1`–`9` select the tabs in order.

A rounded frame encloses the table.
The top edge of the frame shows the title and the PR count of the active view, for example `Opened by me (4)`.
When a filter is active, the top edge shows the matches, for example `Opened by me (2 of 4)`.

A view with no pull requests shows text for that view and the next keys in the middle of the frame:

| View | Text |
| --- | --- |
| **Opened by me** | `You have no open pull requests.` |
| **Review requested** | `No open pull requests wait for your review.` |
| **All open** | `No open pull requests are in the configured scopes.` |
| A view that you add | `No pull requests match "<query>".` |

A default view keeps its text while it keeps the default query and scope.
A default view with a changed query or scope names the query.
Each empty view names `Tab` for the next view, `E` to edit the configuration, and `Ctrl+R` to refresh.
The board omits `Tab` when the configuration has one view.
The text wraps on narrow terminals. No width removes a key.
A short terminal makes the text shorter. Then it shows the keys without the actions.
The filter, loading, and error messages replace this text.
The footer stays on the last rows of the terminal in each of these states.

The board shows the URL of the selected PR.
The selected row has a background color across the full table width.
A `▌` mark in the left gutter identifies the selected row.

When all rows in the current view belong to one repository, the board hides the **REPOSITORY** column.
A bold line in the accent color under the header names the repository and its PR count, for example `acme/api (4)`.
The filter can cause this change. A short terminal keeps the column instead of the line.

The **UPDATED** column shows the time since the last PR update: `now`, `5m`, `3h`, `2d`, or `6w`.
The ages align to the right edge of the table.

The CI column uses a symbol and a color:

| Symbol | Color | Meaning |
| --- | --- | --- |
| `✓` | Green | All checks passed. |
| `●` | Yellow | One or more checks are pending. |
| `✗` | Red | One or more checks failed. |
| `·` | Dim | The PR has no checks, or the plugin cannot get the check status. |

The **REVIEW** column shows local PR Board reviews for the current revision.
A completed review with findings shows the most severe finding level and its count:

```
● P2 1
```

The color identifies the severity. P0 is red. P1 is orange. P2 is yellow. P3 is gray.
A completed review with no findings shows `✓ clean`.
Every other state uses these symbols:

| Symbol | Meaning |
| --- | --- |
| `◉` | A review is running. |
| `●` | A review is queued, waiting for a slot, or awaiting dispatch. |
| `✗` | The current revision has a failed, blocked, or abandoned review. |
| `·` | No local review is active or complete for this revision, or the local review status is unavailable. |

`◉` has the pending color, but its shape is different from pending CI.

Select a PR to read its local reviews in the review region below the URL.
The bottom edge of the frame closes the table above the URL, so the table and the region read as two areas.
The region shows one labeled row for each topic:

| Label | Content |
| --- | --- |
| `REVIEW` | The outcome, the reviewer, and the revision comparison. The run position, for example `run 1 of 3`, is at the right edge. |
| `FINDINGS` | The reviewer summary. Each finding follows under its severity label, with its file, line, and text. |
| `REVISION` | The short head commit, the target branch, and the start time. |
| `POSTED` | The publication results of this board, for example `comment published`, and the submitted reviews for the PR. |
| `AUTOMATION` | The monitor state, the automatic launch setting, and the action after a review. |

When a newer run did not complete, the region also shows the completed run that publication targets.
The run ID and the diagnostics path show only in zoom.
A terminal with fewer than 24 rows collapses the region to one summary line.
That line shows the same severity text as the REVIEW column, for example `● P2 1` or `✓ clean`.
Local status updates each second without GitHub requests.

The **POSTED** column shows submitted reviews from your authenticated GitHub account:

| Label | Meaning |
| --- | --- |
| `GitHub` | Reviews posted outside this PR Board installation. |
| `PR Board` | Reviews matched to this installation's publication records. |
| `Both` | Both sources have submitted reviews. |
| `·` | No submitted reviews were found, or the review history, publication origin, or a publication outcome is uncertain. |

The selected detail names the reason for `·`.

The selected detail shows whether each source has a review on the current head.
Otherwise, it identifies older reviews or an unknown revision.
A newer commit does not inherit a completed local review.
Draft reviews and ordinary PR conversation comments do not count as submitted reviews.
Dismissed reviews still count as posts. The column does not represent approval status.
`GitHub` identifies the publication source. It does not prove that a human wrote the review.
A review started with `--review` is still a PR Board review.
Press `v` to zoom the review region for previous runs, run IDs, diagnostics paths, and publication diagnostics.
GitHub review observations follow the normal refresh interval and cache.

## Controls

| Key | Action |
| --- | --- |
| Left click a view | Select the view. |
| Left click a PR | Select the PR. A click on the selected PR opens it in a browser. |
| Left click the URL | Open the PR in a browser. |
| Left click a footer key | Do the action of that key. |
| Mouse wheel | Over the table, move through the PR list. Over the review region, scroll the region. |
| `1`–`9` | Select a view. |
| `Tab`, `Shift+Tab`, `h`, `l`, `←`, `→` | Select the next or previous view. |
| `↑`, `↓` | Select a PR. |
| `Home` | Select the first PR. |
| `End` | Select the last PR. |
| `j`, `k` | Scroll the review region by one line. |
| `g`, `G` | Scroll to the start or the end of the review region. |
| `PgUp`, `PgDn` | Scroll the review region by one page. |
| `/` | Start filter input. |
| `↑`, `↓` | During filter input, select a PR. |
| `Enter` | Keep the filter and finish filter input. |
| `Backspace` | Remove the last filter character. When the filter is empty, finish filter input. |
| `Ctrl+U`, `Esc` | Clear the filter. |
| `E` | Edit the active configuration. |
| `v` | Zoom the review region to the full height. |
| `t`, `s`, `c`, `a`, `x` | Act on the selected PR's reviews. See [review region](reviews.md#use-the-review-region). |
| A command shortcut key | Run the command shortcut for the selected PR. See [command shortcuts](#command-shortcuts). |
| `Ctrl+R` | Refresh the active view. |
| `R` | Refresh all views. |
| `Enter`, `o` | Open the selected PR in a browser. |
| `?` | Open or close the keyboard help. |
| `q`, `Ctrl+C` | Close the board. |

The board sorts PRs by update time, with the most recent first.
The filter matches repository names, titles, authors, and PR numbers.
It also matches the REVIEW and CI cells and their state names.
For example, `/p0` finds a review whose most severe finding is P0, and `/clean` finds a clean review.
`/failure` finds failed CI.
The filter ignores letter case and makes no GitHub requests.
One filter applies to all views.
The filter and its match count start the last line, for example `/ rev · 2 of 4`.
An armed publication confirmation comes before the filter, so a long filter cannot hide it.
The filter takes at most half of the line, so notices and warnings stay visible.
The filter does not move the table or the footer controls.
A refresh keeps the selection on the same PR while the PR stays in the view.

Press `E` to edit the active configuration while the board runs.
The board uses `$VISUAL`, then `$EDITOR`.
The fallback editor is Notepad on Windows and `vi` on macOS and Linux.
The footer names the editor before it starts. For example: `Opening config in vi. Set $VISUAL or $EDITOR to change.`
The board also writes this line to the terminal that the editor uses.
It validates the file after the editor exits. It reloads valid changes and refreshes all views.
It keeps the previous configuration when the editor or validation fails.

The board reports browser failures at the bottom of the screen.
The selected URL stays visible for manual copying.

The footer shows the most used controls. On narrow terminals, the pairs wrap.
A short pane shows the keys without the actions.
Click a footer key to do the action of that key. The click is the same as the key press.
A footer entry with two keys, such as `j/k`, does not respond to a click.

The last line shows the time since the refresh and the remaining GitHub rate limits, for example `updated 5m · Search 27/30 · GraphQL 4823/5000`.
This status is dim.
The warning color identifies an item that needs attention: a notice, a warning, a stale view, or a low rate limit.
Notices, warnings, and the stale mark come before the refresh time and the rate limits.
A Search rate limit is low when it cannot pay for a refresh of all views.
A GraphQL rate limit is low when it cannot pay for one more CI batch.

Press `?` for the keyboard help.
The help lists every board control, every review region control, and every command shortcut.
Scroll it with `j`, `k`, `↑`, `↓`, `g`, `G`, `Home`, `End`, or the mouse wheel.
Press `?` or `Esc` to close it.

See [reviews](reviews.md) for review controls, cancellation, and result history.

## Command shortcuts

A command shortcut runs your command for the selected PR in a new Herdr pane.
Add `[[shortcuts]]` tables to `config.toml`. See [command shortcuts](configuration.md#command-shortcuts).
No command shortcut is active by default.

Press the shortcut key on the board or in zoom.
In zoom, the shortcut uses the zoomed PR.
The footer shows each shortcut as its key and name. Click it to run the shortcut.
The shortcut key does not run during filter input or a publication confirmation.

A `tab` shortcut opens a new focused Herdr tab. The tab label is `<name> <owner/repo>#<number>`.
A `split` shortcut splits the board pane to the right and focuses the new pane.
The new pane runs the command under `sh -c`.
After the launch, the footer shows a notice, for example `Pi review → owner/repo#12`.
A launch failure shows a warning in the footer.
The board does not track the new pane. The shortcut does not change the REVIEW column.

## Open the board with a shortcut

Run this command:

```sh
herdr plugin action invoke open --plugin cdowell09.pr-board
```

The action opens a dedicated tab named `PR Board`.
If the tab is open, the action focuses that tab.

You can add this key binding to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+shift+b"
type = "plugin_action"
command = "cdowell09.pr-board.open"
description = "open PR board"
```

Reload the Herdr configuration:

```sh
herdr server reload-config
```

The default Herdr prefix is `Ctrl+B`. Press `Ctrl+B`, and then press `Shift+B` to open the board.

The shortcut does not replace `prefix+shift+p`. Herdr uses that shortcut to rename a pane.

## Layouts

The board adapts to the terminal width:

| Terminal width | Layout |
| --- | --- |
| 120 cells or more | All columns. |
| 100–119 cells | All columns with compact repository and author columns. |
| 80–99 cells | No author column. |
| 60–79 cells | No author or updated columns. |
| Fewer than 60 cells | PR, CI, REVIEW, and title columns. |

The REVIEW column keeps 8 cells at every width. The title column absorbs the remaining width.
The fixed columns stay at the right edge of the table.
The title bar shows the plugin version at 80 cells or more.
A narrower terminal omits the version and keeps the configured title.
The selected PR URL stays visible at every width.
A terminal with 24 rows or more shows the review region under the URL.
The table keeps its rows up to half the screen.
The region takes the rest and keeps at least six rows.
A shorter terminal shows the one-line summary under the URL instead.
The summary wraps at the terminal width.
The board truncates column text by terminal cell width.
Emoji, combining characters, and wide glyphs stay aligned.

![Narrow board layout](images/layout-narrow.png)

![Medium board layout](images/layout-medium.png)

![Wide board layout](images/layout-wide.png)
