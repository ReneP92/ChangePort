#!/usr/bin/env bash
# Publishes the built landing site (site/dist, from just site-build) to its
# bucket behind CloudFront (terraform/modules/static-site), as
# .github/workflows/publish-site.yml does from main:
#
# 1. Uploads every file with its content type. Assets carry their content's
#    hash in their names, so browsers keep them for a year; everything else
#    is revalidated by browsers on each visit and kept by CloudFront until
#    the next publish.
# 2. Removes the pages and files the build no longer has, then empties
#    CloudFront's cache and waits until every edge serves the new pages.
# 3. Only then removes the assets the new pages no longer use, which the old
#    pages, still cached until a moment ago, did.
#
# Usage: hack/publish-site.sh <dist> <bucket> <distribution-id>
set -euo pipefail

dist=${1:?usage: hack/publish-site.sh <dist> <bucket> <distribution-id>}
bucket=s3://${2:?usage: hack/publish-site.sh <dist> <bucket> <distribution-id>}
distribution=${3:?usage: hack/publish-site.sh <dist> <bucket> <distribution-id>}

# The content type of each kind of file the site has. A file of another kind
# stops the publish, rather than reaching browsers with a guessed type.
declare -A types=(
  [html]="text/html; charset=utf-8"
  [js]="text/javascript; charset=utf-8"
  [css]="text/css; charset=utf-8"
  [txt]="text/plain; charset=utf-8"
  [svg]="image/svg+xml"
  [webp]="image/webp"
  [png]="image/png"
  [woff2]="font/woff2"
  [woff]="font/woff"
)
immutable="public, max-age=31536000, immutable"
revalidate="public, max-age=0, s-maxage=31536000, must-revalidate"

unknown=$(find "$dist" -type f | sed -n 's/.*\.\([^./]*\)$/\1/p' | sort -u | while read -r extension; do
  [[ -v "types[$extension]" ]] || echo "$extension"
done)
if [[ -n $unknown ]] || find "$dist" -type f ! -name '*.*' | grep -q .; then
  echo "publish-site: no content type for these files: ${unknown:-(without an extension)}" >&2
  exit 1
fi

# The assets first, so that a page CloudFront fetches meanwhile finds every
# asset it names.
for extension in "${!types[@]}"; do
  aws s3 sync "$dist/assets" "$bucket/assets" --only-show-errors \
    --exclude '*' --include "*.$extension" \
    --content-type "${types[$extension]}" --cache-control "$immutable"
done
for extension in "${!types[@]}"; do
  aws s3 sync "$dist" "$bucket" --only-show-errors \
    --exclude '*' --include "*.$extension" --exclude 'assets/*' \
    --content-type "${types[$extension]}" --cache-control "$revalidate"
done
# Everything is uploaded, so this only deletes.
aws s3 sync "$dist" "$bucket" --only-show-errors --delete --exclude 'assets/*'

invalidation=$(aws cloudfront create-invalidation --distribution-id "$distribution" \
  --paths '/*' --query Invalidation.Id --output text)
echo "publish-site: waiting for CloudFront to empty its cache ($invalidation)"
# The CLI gives up after 10 minutes, which an invalidation now and then takes.
for attempt in 1 2 3; do
  if aws cloudfront wait invalidation-completed --distribution-id "$distribution" --id "$invalidation"; then
    break
  fi
  if ((attempt == 3)); then
    echo "publish-site: CloudFront has not emptied its cache after 30 minutes; the old assets stay" >&2
    exit 1
  fi
done

aws s3 sync "$dist/assets" "$bucket/assets" --only-show-errors --delete
echo "publish-site: published $dist to $bucket"
