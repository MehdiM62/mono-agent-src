package vault

// Windows has no O_NOFOLLOW; copyFileOpt's Lstat check is the guard there.
const oNoFollow = 0
