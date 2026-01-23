import { useMemo, useState, useRef, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ArrowUp } from 'lucide-react'
import { fetchStats } from '@/lib/api'
import { StatCard } from '@/components/stat-card'
import { useTheme } from '@/hooks/use-theme'

const EXAMPLE_QUESTIONS = [
  'How is the network doing?',
  'Compare DZ to the public internet',
  'Which validators connected recently?',
  'Which metros have the most validators?',
  'Show me link latency by metro pair',
  'Which links have the highest utilization?',
  // Graph/topology questions
  'If the Hong Kong device goes down, what metros lose connectivity?',
  'What metros can I reach from Singapore?',
  'Show the paths between NYC and LON',
]

function selectRandom<T>(arr: T[], n: number): T[] {
  const shuffled = [...arr]
  for (let i = shuffled.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[shuffled[i], shuffled[j]] = [shuffled[j], shuffled[i]]
  }
  return shuffled.slice(0, n)
}

export function Landing() {
  const navigate = useNavigate()
  const { resolvedTheme } = useTheme()
  const [input, setInput] = useState('')
  const inputRef = useRef<HTMLTextAreaElement>(null)

  const { data: stats } = useQuery({
    queryKey: ['stats'],
    queryFn: fetchStats,
    refetchInterval: 15_000,
    staleTime: 10_000,
  })

  const exampleQuestions = useMemo(() => selectRandom(EXAMPLE_QUESTIONS, 3), [])

  // Only auto-focus on desktop to avoid scroll-to-input on mobile
  useEffect(() => {
    const isDesktop = window.matchMedia('(hover: hover) and (pointer: fine)').matches
    if (isDesktop && inputRef.current) {
      inputRef.current.focus()
    }
  }, [])

  const handleStartChat = (question?: string) => {
    const q = question || input.trim()
    if (q) {
      navigate(`/chat?q=${encodeURIComponent(q)}`)
    } else {
      navigate('/chat')
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (input.trim()) {
        handleStartChat()
      }
    }
  }

  return (
    <div className="flex-1 flex flex-col items-center justify-start px-8 py-12 overflow-auto">
      <div className="flex-1 flex flex-col items-center justify-center w-full">
      {/* Header */}
      <div className="text-center mb-16">
        <img
          src={resolvedTheme === 'dark' ? '/logoDark.svg' : '/logoLight.svg'}
          alt="DoubleZero"
          className="h-8 mx-auto mb-3"
        />
        <p className="text-muted-foreground">
          Real-time insights into the DoubleZero network
        </p>
      </div>

      {/* Stats Grid */}
      <div className="grid grid-cols-2 sm:grid-cols-5 gap-x-10 gap-y-8 mb-16 max-w-5xl w-full">
        {/* Row 1: Network Infrastructure */}
        <StatCard
          label="Contributors"
          value={stats?.contributors}
          format="number"
          href="/dz/contributors"
        />
        <StatCard
          label="Metros"
          value={stats?.metros}
          format="number"
          href="/dz/metros"
        />
        <StatCard
          label="Devices"
          value={stats?.devices}
          format="number"
          href="/dz/devices"
        />
        <StatCard
          label="Links"
          value={stats?.links}
          format="number"
          href="/dz/links"
        />
        <StatCard
          label="Users"
          value={stats?.users}
          format="number"
          href="/dz/users"
        />
        {/* Row 2: Solana + Traffic */}
        <StatCard
          label="Validators on DZ"
          value={stats?.validators_on_dz}
          format="number"
          href="/solana/validators"
        />
        <StatCard
          label="SOL Connected"
          value={stats?.total_stake_sol}
          format="stake"
        />
        <StatCard
          label="Stake Share"
          value={stats?.stake_share_pct}
          format="percent"
        />
        <StatCard
          label="Capacity"
          value={stats?.bandwidth_bps}
          format="bandwidth"
        />
        <StatCard
          label="User Inbound"
          value={stats?.user_inbound_bps}
          format="bandwidth"
        />
      </div>

      {/* Prompt Input */}
      <div className="w-full max-w-2xl">
        <div className="relative rounded-[24px] border border-border bg-secondary overflow-hidden">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Ask about the network..."
            rows={1}
            className="w-full bg-transparent px-4 pt-3.5 pb-2.5 pr-12 text-sm placeholder:text-muted-foreground focus:outline-none resize-none min-h-[44px] max-h-[200px] overflow-y-auto"
            style={{ height: 'auto' }}
            onInput={(e) => {
              const target = e.target as HTMLTextAreaElement
              target.style.height = 'auto'
              target.style.height = Math.min(target.scrollHeight, 200) + 'px'
            }}
          />
          <button
            onClick={() => handleStartChat()}
            disabled={!input.trim()}
            className="absolute right-2 bottom-3 p-1.5 rounded-full bg-accent text-white hover:bg-accent-orange-100 disabled:bg-muted-foreground/30 disabled:cursor-not-allowed transition-colors"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
        </div>
      </div>

      {/* Example questions */}
      <div className="mt-4 flex flex-wrap justify-center gap-2 max-w-xl">
        {exampleQuestions.map((question) => (
          <button
            key={question}
            onClick={(e) => {
              if (e.metaKey || e.ctrlKey) {
                window.open(`/chat?q=${encodeURIComponent(question)}`, '_blank')
              } else {
                handleStartChat(question)
              }
            }}
            className="px-3 py-1.5 text-sm border border-border rounded-full hover:bg-secondary hover:border-muted-foreground/30 transition-colors text-muted-foreground hover:text-foreground"
          >
            {question}
          </button>
        ))}
      </div>
      </div>
    </div>
  )
}
