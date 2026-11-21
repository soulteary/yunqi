# Homebrew Tap Template

This directory contains the template files for the Homebrew Tap repository
`BenedictKing/homebrew-yunqi`. These files are maintained here as a reference;
the actual Tap is a separate GitHub repository.

## Setup

1. Create a new GitHub repository: `BenedictKing/homebrew-yunqi`
2. Copy `update-cask.yml` into the Tap repo as:
   ```
   .github/workflows/update-cask.yml
   ```
3. In the main `BenedictKing/yunqi` repo, add a secret:
   - `HOMEBREW_TAP_TOKEN` — a PAT with `repo` scope that can trigger
     `repository_dispatch` on the Tap repo.
4. Run the main repo `Update Homebrew Tap` workflow manually with an existing
   published release tag, for example `v2.8.17`. This dispatches the Tap
   workflow and generates the first real `Casks/yunqi-desktop.rb` file.

`Casks/yunqi-desktop.rb.template` is only a reference template. Do not copy it
as the final Cask without replacing the placeholder version and SHA256 values.

## User Installation

```bash
brew tap BenedictKing/yunqi
brew install --cask yunqi-desktop
```

## Updates

The Cask is automatically updated after a GitHub Release is published in the
main repository. The `Update Homebrew Tap` workflow downloads DMG SHA256
checksums and sends a `repository_dispatch` event to the Tap, which rewrites
`Casks/yunqi-desktop.rb` with the new version and checksums.
