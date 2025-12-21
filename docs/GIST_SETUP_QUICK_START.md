# Quick Start: Fix "404 Not Found" Gist Error

## Problem

You're seeing this error in GitHub Actions:
```
Error: Failed to get gist: 404 Not Found
```

This means the Gist for the coverage badge hasn't been created yet, or the secrets aren't configured.

## Quick Fix Options

### Option 1: Skip Gist Setup (Recommended for Now)

The workflow is now configured to work without the Gist. You can:

1. **Use Codecov instead** (already in your workflow)
   - Sign up at https://codecov.io
   - Link your repository
   - Badge will work automatically

2. **Check coverage locally**
   ```bash
   make coverage
   # or
   make coverage-html
   ```

### Option 2: Set Up Gist (5 minutes)

If you want the self-hosted Shields.io badge:

#### Step 1: Create Personal Access Token

1. Go to: https://github.com/settings/tokens
2. Click "Generate new token (classic)"
3. Name it: `SmartHPA Coverage Badge`
4. Check only the `gist` scope
5. Click "Generate token"
6. **Copy the token** (e.g., `ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxx`)

#### Step 2: Create the Gist

1. Go to: https://gist.github.com/new
2. Description: `SmartHPA Coverage Badge Data`
3. Filename: `smarthpa-coverage.json`
4. Content:
   ```json
   {
     "schemaVersion": 1,
     "label": "coverage",
     "message": "50%",
     "color": "yellow"
   }
   ```
5. Click "Create public gist"
6. **Copy the Gist ID** from URL (e.g., URL is `https://gist.github.com/sarabala1979/abc123def456`, Gist ID is `abc123def456`)

#### Step 3: Add Secrets to Repository

1. Go to your repository on GitHub
2. Click: **Settings** → **Secrets and variables** → **Actions**
3. Click "New repository secret"
4. Add first secret:
   - Name: `GIST_TOKEN`
   - Value: (paste the token from Step 1)
   - Click "Add secret"
5. Add second secret:
   - Name: `GIST_ID`
   - Value: (paste the Gist ID from Step 2)
   - Click "Add secret"

#### Step 4: Update README

1. Edit `README.md`
2. Uncomment the coverage badge line
3. Replace `YOUR_GIST_ID` with your actual Gist ID from Step 2
4. Commit and push

#### Step 5: Verify

1. Push a commit to main branch
2. Go to Actions tab
3. Watch the build complete
4. Check that "Create Coverage Badge (Gist)" step succeeds
5. Refresh your README to see the badge

## Verification Commands

```bash
# Check if secrets are set (in GitHub Actions logs)
echo "GIST_TOKEN set: ${{ secrets.GIST_TOKEN != '' }}"
echo "GIST_ID set: ${{ secrets.GIST_ID != '' }}"

# Test locally
make coverage

# View HTML report
make coverage-html
```

## Common Issues

### Issue: "404 Not Found"
**Cause**: Gist doesn't exist or ID is wrong
**Fix**: 
- Verify Gist exists at: https://gist.github.com/YOUR_USERNAME/YOUR_GIST_ID
- Check GIST_ID secret matches the URL

### Issue: "401 Unauthorized"
**Cause**: Token is invalid or doesn't have gist scope
**Fix**: 
- Create new token with `gist` scope
- Update GIST_TOKEN secret

### Issue: Badge not updating
**Cause**: Gist is private or URL is cached
**Fix**:
- Make Gist public
- Clear browser cache
- Add `?v=1` to badge URL to bypass cache

### Issue: Workflow fails but I don't want Gist
**Fix**: Already handled! The workflow now continues even if Gist fails.
- Just use Codecov or local tools
- The error won't block your builds

## Alternative: Use Codecov Only

If you don't want to deal with Gist setup:

1. Go to: https://codecov.io
2. Sign in with GitHub
3. Add your repository
4. Done! Codecov badge will work automatically

The badge in your README:
```markdown
[![codecov](https://codecov.io/gh/sarabala1979/SmartHPA/branch/main/graph/badge.svg)](https://codecov.io/gh/sarabala1979/SmartHPA)
```

## Need Help?

1. Read full guide: `docs/COVERAGE_BADGE_SETUP.md`
2. Check workflow logs in GitHub Actions
3. Open an issue with error details

## TL;DR

**Don't want to set up Gist?**
- You're good! Use `make coverage` locally
- Codecov will work automatically
- The 404 error won't block your builds

**Want self-hosted badge?**
- Create PAT with gist scope
- Create Gist with coverage JSON
- Add GIST_TOKEN and GIST_ID secrets
- Update README with your Gist ID

