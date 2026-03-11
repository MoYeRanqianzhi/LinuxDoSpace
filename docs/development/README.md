# LinuxDoSpace Development Notes

This directory stores the durable development documentation for LinuxDoSpace.
Use it to keep architecture, deployment, API, and release knowledge traceable across iterations.

## Suggested reading order

1. [architecture.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/architecture.md)
2. [api.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/api.md)
3. [runbook.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/runbook.md)
4. [deployment.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/deployment.md)
5. [references.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/references.md)
6. [known-issues.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/known-issues.md)
7. [changelog.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/changelog.md)

## Current project state

- The Go backend already provides Linux Do OAuth, server-side sessions, CSRF checks, managed-domain allocation, DNS record management, and administrator APIs.
- SQLite persistence, Cloudflare integration, Docker packaging, and GHCR-based release workflows are in place.
- The main frontend is connected to the backend and supports the current public/user flows.
- The standalone `admin-frontend/` project is no longer just a UI prototype. It now uses real backend administrator sessions and management APIs.
