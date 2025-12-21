#!/bin/bash
# Script to calculate and display test coverage for SmartHPA

set -e

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo "🧪 Running tests with coverage..."
make test > /dev/null 2>&1 || {
    echo -e "${RED}❌ Tests failed!${NC}"
    exit 1
}

echo -e "${GREEN}✅ Tests passed!${NC}\n"

# Calculate overall coverage
echo "📊 Coverage Report:"
echo "===================="

# Get detailed coverage by package
go tool cover -func=cover.out | grep -v "total:" | while read -r line; do
    if [[ $line == *".go"* ]]; then
        pkg=$(echo "$line" | awk '{print $1}' | cut -d'/' -f1-6 | cut -d':' -f1)
        echo "$pkg"
    fi
done | sort -u | while read -r pkg; do
    coverage=$(go tool cover -func=cover.out | grep "$pkg" | grep "total:" | awk '{print $3}')
    if [ ! -z "$coverage" ]; then
        pkg_name=$(basename "$pkg")
        printf "%-30s %s\n" "$pkg_name:" "$coverage"
    fi
done

echo "===================="

# Get total coverage
TOTAL_COVERAGE=$(go tool cover -func=cover.out | grep total | awk '{print $3}')
COVERAGE_NUM=$(echo $TOTAL_COVERAGE | sed 's/%//')

echo ""
printf "Total Coverage: "

# Color code based on coverage percentage
if (( $(echo "$COVERAGE_NUM >= 80" | bc -l) )); then
    echo -e "${GREEN}$TOTAL_COVERAGE 🎉${NC}"
elif (( $(echo "$COVERAGE_NUM >= 60" | bc -l) )); then
    echo -e "${GREEN}$TOTAL_COVERAGE ✅${NC}"
elif (( $(echo "$COVERAGE_NUM >= 40" | bc -l) )); then
    echo -e "${YELLOW}$TOTAL_COVERAGE ⚠️${NC}"
else
    echo -e "${RED}$TOTAL_COVERAGE ❌${NC}"
fi

echo ""
echo "📁 Detailed HTML report: cover.html"
go tool cover -html=cover.out -o cover.html

echo ""
echo "💡 Tips to improve coverage:"
echo "   - Add more unit tests for uncovered functions"
echo "   - Add integration tests for controller logic"
echo "   - Add e2e tests for complete workflows"
echo ""
echo "🌐 Open cover.html in your browser for detailed coverage analysis"

