# Embassy bot

- Maintains a FIFO review queue.
- Welcomes first-time contributors with a link to the contributor guide
- Warns draft PRs aren't added to the review queue.
- Warns PRs with red CI aren't added to the review queue (only if CI is still red 1h after it went red; a build that's merely still running doesn't count).
- Probably more in the future.
