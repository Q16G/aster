---
name: agent-browser
description: Browser automation CLI for AI agents. Use when the user needs to interact with websites, fill forms, click buttons, extract data, take screenshots, or automate any browser task.
tags: browser,automation,web
when-to-use: 当需要通过浏览器自动化访问网页、交互、截图、提取数据时
allowed-tools: bash,read_file,list_files,rg,list_skills,load_skills
user-invocable: true
argument-hint: "<url>"
arguments:
  - target_url
---

# agent-browser

Fast browser automation CLI for AI agents. Chrome/Chromium via CDP with
accessibility-tree snapshots and compact `@eN` element refs.

## Start here

This file is a discovery stub, not the usage guide. Before running any
`agent-browser` command, load the actual workflow content from the CLI:

```bash
agent-browser skills get core          # start here
agent-browser skills get core --full   # include full command reference
```

## Specialized skills

```bash
agent-browser skills get dogfood       # exploratory testing / QA
agent-browser skills get electron      # Electron desktop apps
agent-browser skills get slack         # Slack workspace automation
agent-browser skills get agentcore     # AWS Bedrock cloud browsers
agent-browser skills get vercel-sandbox # Vercel Sandbox microVMs
```

## Security testing

When loaded for penetration testing / security assessment, the testing workflow
and security-specific rules are provided by `web-security-testing` skill.
Load that skill for the complete security browser workflow.
