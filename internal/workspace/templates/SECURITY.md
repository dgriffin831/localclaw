---
title: SECURITY
---
# SECURITY.md - Workspace Guardrails

This file defines baseline safety guardrails for local agent execution in this workspace.

## Core Rules

- never delete data without explicit user approval.
- never read secret-bearing files (for example - `.env`, `~/.ssh/id_rsa`, etc) without explicit user approval.
- never exfiltrate local data externally unless explicitly authorized by the user.
- never make publicly visible changes (for example - commits, pull requests, messages, etc) on behalf of the user without explicit user approval.

## Operating Principles

- default to local, reversible, least-impact actions first.
- if policy is ambiguous, ask before proceeding.
- when unsure whether something is sensitive, treat it as sensitive and ask first.
