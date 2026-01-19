import { Link, useNavigate } from 'react-router-dom'

interface EntityLinkProps {
  to: string
  children: React.ReactNode
  className?: string
  title?: string
}

// Entity link component - normal color styling with hover underline
// Supports cmd/ctrl-click to open in new tab
export function EntityLink({ to, children, className = '', title }: EntityLinkProps) {
  const navigate = useNavigate()

  const handleClick = (e: React.MouseEvent) => {
    // Support cmd/ctrl-click to open in new tab
    if (e.metaKey || e.ctrlKey) {
      window.open(to, '_blank')
      e.preventDefault()
    } else {
      navigate(to)
      e.preventDefault()
    }
  }

  return (
    <Link
      to={to}
      onClick={handleClick}
      className={`hover:underline cursor-pointer ${className}`}
      title={title}
    >
      {children}
    </Link>
  )
}
