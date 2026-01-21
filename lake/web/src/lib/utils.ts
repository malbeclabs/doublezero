import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function handleRowClick(
  e: React.MouseEvent,
  url: string,
  navigate: (url: string) => void
): void {
  if (e.metaKey || e.ctrlKey) {
    window.open(url, '_blank')
  } else {
    navigate(url)
  }
}
