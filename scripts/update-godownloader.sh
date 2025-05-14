#!/bin/sh -ex
#
# This updates the 'godownloader-*.sh' scripts from upstream
# This is done manually
#
SOURCE=https://raw.githubusercontent.com/goreleaser/godownloader/head/samples
curl --fail -o godownloader-misspell.sh "$SOURCE/godownloader-misspell.sh"
chmod a+x godownloader-misspell.sh

