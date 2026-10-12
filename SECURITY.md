# Security

Astrolabe CLI reads and writes Markdown files in a folder you choose and makes no network requests. The surface that matters is what it parses: Markdown and frontmatter from notes you did not write, and the terminal it draws into.

## Reporting a vulnerability

Please report privately through [GitHub's vulnerability reporting](https://github.com/ZahakJ/astrolabe-cli/security/advisories/new) rather than in a public issue. Include the version (`astrolabe --version`), the terminal, the steps or a note that triggers the problem, and what an attacker gains. You will get an acknowledgement within a week and a fix or a reasoned answer as soon as one exists. There is no bounty programme.

## What is defended

- Writes are atomic (a temporary file in the same directory, synced, then renamed over the original) and the tool writes only the notes you edit or capture into; `docs/obsidian.md` lists the data-safety guarantees.
- Release binaries are built by the repository's workflow and published with `checksums.txt`; `install.sh` verifies the download against it. The binaries are not signed.

## Supported versions

The latest release only.
