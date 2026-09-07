# Delegate review execution

PR Board must own PR discovery, review eligibility, and work tracking.
An external reviewer must perform review execution.
Users must be able to select their reviewer command.
Pi is the first reference adapter, not a core dependency.
This separation lets users replace the reviewer without changing PR discovery or work tracking.
Embedding a reviewer would couple these functions to one execution environment.
