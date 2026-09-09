# Own the review appliance

PR Board must own PR discovery, review dispatch, review findings, and publication records.
The user's own coding agent must own fixes to a PR and follow-up on PRs the user authored.
PR Board must not launch, prompt, or drive that coding agent.
PR Board must not become a general GitHub interface for other agents.
The JSON snapshot remains available for scripts and stays a secondary surface.
This boundary keeps PR Board small and lets each user keep their preferred coding agent.
Owning fixes or authored-PR follow-up would duplicate work that GitHub and the coding agent already do.
See [core user journeys](../journeys.md).
