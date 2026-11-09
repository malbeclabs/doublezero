package shreds

import "github.com/gagliardetto/solana-go"

// FeedProgramID is the feed subscription program ID. On a Solana cluster
// without the program, getProgramAccounts returns an empty list rather than an
// error.
var FeedProgramID = solana.MustPublicKeyFromBase58("J9gupbyffs4XAoKn5NrJ4hrbdqW5ZfvMDaaas3FtH8yC")
