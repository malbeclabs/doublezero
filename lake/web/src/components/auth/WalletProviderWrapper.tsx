import { useMemo } from 'react'
import type { ReactNode } from 'react'
import { ConnectionProvider, WalletProvider } from '@solana/wallet-adapter-react'
import { WalletModalProvider } from '@solana/wallet-adapter-react-ui'
import { PhantomWalletAdapter, SolflareWalletAdapter } from '@solana/wallet-adapter-wallets'

// Import wallet adapter styles
import '@solana/wallet-adapter-react-ui/styles.css'

interface WalletProviderWrapperProps {
  children: ReactNode
}

// Using Solana mainnet-beta for production
const SOLANA_RPC_ENDPOINT = 'https://api.mainnet-beta.solana.com'

export function WalletProviderWrapper({ children }: WalletProviderWrapperProps) {
  // Initialize wallet adapters
  const wallets = useMemo(
    () => [
      new PhantomWalletAdapter(),
      new SolflareWalletAdapter(),
    ],
    []
  )

  return (
    <ConnectionProvider endpoint={SOLANA_RPC_ENDPOINT}>
      <WalletProvider wallets={wallets} autoConnect={false}>
        <WalletModalProvider>
          {children}
        </WalletModalProvider>
      </WalletProvider>
    </ConnectionProvider>
  )
}
