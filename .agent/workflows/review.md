---
name: Code Review
description: Performs a comprehensive Code Review comparing the current state with the main branch.
---

1. Detect the Operating System.
2. Invoke the Code Review skill located at `"$HOME\.agent\skills\code_review\SKILL.md"` (Windows) or `"$HOME/.agent/skills/code_review/SKILL.md"` (Unix).
3. Execute the `get_changes` script defined in the skill for your OS.
4. Generate the report in Spanish.
