# Docs

This documentation set is for contributors working on the Nexus boilerplate itself.

Nexus is a reusable full-stack foundation built around:

- a PocketBase-based hub
- an outbound WebSocket/CBOR agent
- a React/Vite frontend
- deployment and packaging helpers under `supplemental/`

Use this folder when you need implementation context, extension guidance, or task-oriented navigation. Keep the top-level `README.md` short and external-facing; keep project-internal knowledge here.

## Start Here

- Read `project-overview.md` for the product and boilerplate mental model.
- Read `architecture/hub-agent-architecture.md` for runtime architecture and protocol flow.
- Read `development/workflow-and-testing.md` before building, running, or testing changes.

## Navigate By Task

### I need to understand the project

- `project-overview.md`
- `architecture/hub-agent-architecture.md`

### I need to change the hub backend

- `architecture/hub-agent-architecture.md`
- `backend/hub-backend.md`
- `architecture/auth-and-data-model.md`

### I need to change the agent or protocol

- `architecture/hub-agent-architecture.md`
- `agent/agent-runtime.md`
- `conventions-and-gotchas.md`

### I need to change the frontend

- `frontend/frontend-app.md`
- `architecture/auth-and-data-model.md`
- `development/workflow-and-testing.md`

### I need to work on auth, users, tokens, or settings

- `architecture/auth-and-data-model.md`
- `backend/hub-backend.md`
- `frontend/frontend-app.md`

### I need to run the app or tests

- `development/workflow-and-testing.md`

### I need to turn Nexus into a derived product

- `customization/renaming-and-derived-projects.md`

### I am an AI agent and need the fastest route to the right files

- `ai/agent-navigation.md`

## Documentation Map

- `project-overview.md`
  - What Nexus is, what stays generic, and current implementation scope.
- `architecture/`
  - Runtime architecture, protocol flow, auth model, and data model.
- `backend/`
  - Hub backend structure and extension points.
- `agent/`
  - Agent runtime, persistence, and handler model.
- `frontend/`
  - Frontend structure, auth flows, stores, and i18n.
- `development/`
  - Build, dev, and test workflows.
- `customization/`
  - Derived-project and renaming guidance.
- `operations/`
  - Deployment, packaging, and release guidance.
- `troubleshooting/`
  - Common failure modes.
- `ai/`
  - Task routing for AI agents.

## Current Scope

The current codebase has a complete hub/agent skeleton and a small but real frontend shell:

- backend auth and PocketBase integration are implemented
- the agent handshake and request/response protocol are implemented
- the frontend currently focuses on login, settings, and agent management
- many deployment paths already exist as examples under `supplemental/`

That means most documentation here should focus on architecture, conventions, extension points, and contributor workflows rather than end-user features.
