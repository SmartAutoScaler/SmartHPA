# Coverage Badge Setup Guide

This guide explains how to set up the test coverage badge for SmartHPA using GitHub Actions and Shields.io.

## Option 1: Using Codecov (Already Configured)

Codecov is already integrated in the workflow. The badge will automatically update when you:
1. Sign up at https://codecov.io
2. Connect your GitHub repository
3. The badge will automatically appear in the README

## Option 2: Using GitHub Gist and Shields.io (Recommended for Self-Hosted)

This option uses GitHub Actions to calculate coverage and store it in a Gist, which Shields.io reads to display the badge.

### Setup Steps:

#### 1. Create a GitHub Personal Access Token (PAT)

1. Go to GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
2. Click "Generate new token (classic)"
3. Give it a name like "SmartHPA Coverage Badge"
4. Select the `gist` scope (this allows creating and updating gists)
5. Click "Generate token"
6. **Copy the token** (you won't be able to see it again!)

#### 2. Create a GitHub Gist

1. Go to https://gist.github.com
2. Create a new gist with:
   - Description: "SmartHPA Coverage Badge Data"
   - Filename: `smarthpa-coverage.json`
   - Content:
     ```json
     {
       "schemaVersion": 1,
       "label": "coverage",
       "message": "0%",
       "color": "red"
     }
     ```
3. Click "Create secret gist" or "Create public gist" (public is fine)
4. **Copy the Gist ID** from the URL (e.g., `https://gist.github.com/username/GIST_ID`)

#### 3. Add Secrets to Your GitHub Repository

1. Go to your repository → Settings → Secrets and variables → Actions
2. Add two repository secrets:
   - Name: `GIST_TOKEN`
     - Value: The PAT you created in step 1
   - Name: `GIST_ID`
     - Value: The Gist ID you copied in step 2

#### 4. Update the README Badge URL

In `README.md`, replace `GIST_ID` in the coverage badge URL with your actual Gist ID:

```markdown
[![Coverage](https://img.shields.io/endpoint?url=https://gist.githubusercontent.com/sarabala1979/YOUR_GIST_ID/raw/smarthpa-coverage.json)](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml)
```

#### 5. Push Changes and Test

1. Commit and push your changes to the main branch
2. The GitHub Action will run and update the gist
3. The badge in your README will automatically update with the coverage percentage

### How It Works

1. **GitHub Actions runs tests** with coverage enabled (`make test` generates `cover.out`)
2. **Calculate coverage percentage** using `go tool cover`
3. **Determine badge color** based on coverage percentage:
   - ≥80%: bright green
   - ≥60%: green
   - ≥40%: yellow
   - <40%: orange
4. **Update the Gist** with the coverage data using `schneegans/dynamic-badges-action`
5. **Shields.io reads the Gist** and generates the badge image
6. **Badge appears in README** automatically

### Customization

You can customize the coverage thresholds and colors in `.github/workflows/build-test.yaml`:

```yaml
- name: Calculate coverage
  id: coverage
  run: |
    COVERAGE=$(go tool cover -func=cover.out | grep total | awk '{print substr($3, 1, length($3)-1)}')
    
    # Customize these thresholds
    if (( $(echo "$COVERAGE >= 80" | bc -l) )); then
      COLOR="brightgreen"
    elif (( $(echo "$COVERAGE >= 60" | bc -l) )); then
      COLOR="green"
    elif (( $(echo "$COVERAGE >= 40" | bc -l) )); then
      COLOR="yellow"
    else
      COLOR="orange"
    fi
```

## Option 3: Using GitHub Actions Artifacts (Alternative)

If you don't want to use Gist, you can use the coverage artifact:

1. The workflow already uploads `badges/coverage.json` as an artifact
2. You can download this artifact and host it somewhere accessible
3. Point Shields.io to that URL

### Example with GitHub Pages:

```yaml
- name: Deploy to GitHub Pages
  if: github.ref == 'refs/heads/main'
  uses: peaceiris/actions-gh-pages@v3
  with:
    github_token: ${{ secrets.GITHUB_TOKEN }}
    publish_dir: ./badges
```

Then use this badge URL:
```markdown
[![Coverage](https://img.shields.io/endpoint?url=https://sarabala1979.github.io/SmartHPA/coverage.json)](https://github.com/sarabala1979/SmartHPA/actions/workflows/build-test.yaml)
```

## Troubleshooting

### Badge not updating
- Check that the GitHub Action completed successfully
- Verify GIST_TOKEN has the correct permissions
- Ensure GIST_ID is correct
- Clear browser cache or add `?v=1` to the badge URL

### Coverage showing 0%
- Ensure `make test` generates `cover.out`
- Check that the coverage calculation step completed successfully
- Review the GitHub Action logs

### Badge shows "invalid"
- Verify the gist is public or accessible
- Check that the JSON format in the gist is correct
- Ensure the Shields.io endpoint URL is correct

## Current Coverage

As of the latest run, SmartHPA has:
- **API (v1alpha1)**: 41.0% coverage
- **Controller**: 36.4% coverage
- **Scheduler**: 78.2% coverage
- **Overall**: ~65% coverage

## Next Steps to Improve Coverage

1. Add more controller tests for edge cases
2. Add tests for API validation logic
3. Add integration tests
4. Add e2e tests

