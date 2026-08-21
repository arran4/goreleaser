#!/bin/bash
# Remove gentoo_revision_test.go because the functions tested there are private procedural functions that were replaced.
rm internal/pipe/gentoo/gentoo_revision_test.go

# Fix publish.go variable usage
sed -i 's/prClient, err := client.NewIfToken(ctx, cl, p.cfg.Raw().Repository.PullRequest.Token)/prCl, err := client.NewIfToken(ctx, cl, p.cfg.Raw().Repository.PullRequest.Token)\n\t\tprClient = prCl/g' internal/pipe/gentoo/publish.go

# Update gentoo_test.go again to use NewGenerator
bash update_gentoo_test.sh
bash fix_gentoo_test_5.sh
bash fix_gentoo_test_26.sh
