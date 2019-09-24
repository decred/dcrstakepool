#!/bin/bash
#
# This script helps prepare pre-zipped static assets for use with reverse proxy
# features like nginx's gzip_static.

echo "Gzipping assets for use with gzip_static..."
find ./public -type f -name "*.gz" -execdir rm {} \;
# Use GNU parallel if it is installed.
if [ -x "$(command -v parallel)" ]; then
    if [ -x "$(command -v 7za)" ]; then
        find ./public -type f -not -name "*.gz" | parallel --will-cite --bar 7za a -tgzip -mx=9 -mpass=13 {}.gz {} > /dev/null
    else
        find ./public -type f -not -name "*.gz" | parallel --will-cite --bar gzip -k9f {} > /dev/null
    fi
elif [ -x "$(command -v 7za)" ]; then
    find ./public -type f -not -name "*.gz" -execdir 7za a -tgzip -mx=9 -mpass=13 {}.gz {} \; > /dev/null    
else
    find ./public -type f -not -name "*.gz" -execdir gzip -k9f {} \; > /dev/null
fi

# Clean up incompressible files.
find ./public -type f -name "*.png.gz" -execdir rm {} \;
find ./public -type f -name "*.eot.gz" -execdir rm {} \;
find ./public -type f -name "*.gz.gz" -execdir rm {} \;
find ./public -type f -name "*.woff*.gz" -execdir rm {} \;
