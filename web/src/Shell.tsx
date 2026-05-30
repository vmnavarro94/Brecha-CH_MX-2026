interface ShellProps {
  children: React.ReactNode
}

export default function Shell({ children }: ShellProps) {
  return (
    <div className="bx-app">
      <div className="bx-main">{children}</div>
    </div>
  )
}
