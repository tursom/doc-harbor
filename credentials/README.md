# Git Credentials

This directory is mounted read-only into the DocHarbor container at `/credentials`.

Supported files:

- `.netrc`: HTTP(S) Git credentials.
- `.git-credentials`: Git credential-store file.
- `.gitconfig`: optional Git config, for example credential helper settings.
- `ssh/`: optional SSH key directory if you prefer project-local deploy keys.

Keep real secrets out of Git. This directory ignores everything except this README and `.gitignore`.
