import { useState } from 'react'
import {
  LayoutDashboard,
  Radio,
  Activity,
  ArrowLeftRight,
  ListChecks,
} from 'lucide-react'

interface NavItem {
  id: string
  icon: React.ReactNode
  label: string
}

const NAV_ITEMS: NavItem[] = [
  { id: 'top', icon: <LayoutDashboard size={19} strokeWidth={1.75} />, label: 'Resumen' },
  { id: 'prices', icon: <Radio size={19} strokeWidth={1.75} />, label: 'Precios en vivo' },
  { id: 'zscore', icon: <Activity size={19} strokeWidth={1.75} />, label: 'Z-score' },
  { id: 'feed', icon: <ArrowLeftRight size={19} strokeWidth={1.75} />, label: 'Oportunidades' },
  { id: 'trades', icon: <ListChecks size={19} strokeWidth={1.75} />, label: 'Ejecuciones' },
]

interface ShellProps {
  children: React.ReactNode
}

export default function Shell({ children }: ShellProps) {
  const [active, setActive] = useState('top')

  function handleNav(id: string) {
    setActive(id)
    const sectionMap: Record<string, string> = {
      top: 'bx-top',
      prices: 'bx-prices',
      zscore: 'bx-zscore',
      feed: 'bx-feed',
      trades: 'bx-trades',
    }
    const el = document.getElementById(sectionMap[id])
    const cont = document.querySelector('.bx-content') as HTMLElement | null
    if (!el || !cont) return
    const target =
      id === 'top'
        ? 0
        : Math.max(
            0,
            cont.scrollTop +
              el.getBoundingClientRect().top -
              cont.getBoundingClientRect().top -
              12
          )
    cont.scrollTo({ top: target, behavior: 'smooth' })
  }

  return (
    <div className="bx-app">
      <nav className="bx-rail">
        {NAV_ITEMS.map((item) => (
          <button
            key={item.id}
            className={'bx-rail-item' + (active === item.id ? ' active' : '')}
            onClick={() => handleNav(item.id)}
            title={item.label}
            aria-label={item.label}
          >
            {item.icon}
            <span className="bx-rail-tip">{item.label}</span>
          </button>
        ))}
      </nav>
      <div className="bx-main">{children}</div>
    </div>
  )
}
