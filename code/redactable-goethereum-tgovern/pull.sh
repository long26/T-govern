
DEFAULT_BRANCH="lwlshiyan"

BRANCH_NAME="${1:-$DEFAULT_BRANCH}"



git pull origin "$BRANCH_NAME"

git reset --hard "origin/$BRANCH_NAME"

git pull origin "$BRANCH_NAME"

make clean

make geth

chmod +x per.sh

./per.sh