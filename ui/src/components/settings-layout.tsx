import { Link, Outlet, useLocation } from 'react-router-dom'
import {Bot, Boxes, Map, ListTodo, Route} from 'lucide-react'
import { buttonVariants } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { cn } from '@/lib/utils'

const items = [
  { title: 'Agents', to: '/settings/agents', icon: Bot },
  { title: 'Agent Groups', to: '/settings/agent-groups', icon: Boxes },
  { title: 'Layouts', to: '/settings/layouts', icon: Map },
    { title: 'Missions', to: '/missions', icon: Route },
    { title: 'Tasks', to: '/tasks', icon: ListTodo },
]

export function SettingsLayout() {
  const { pathname } = useLocation()

  return (
    <div className="flex flex-col gap-6">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">Settings</h1>
        <p className="text-muted-foreground text-sm">
          Manage agents and layouts.
        </p>
      </div>
      <Separator />
      <div className="flex flex-col gap-8 lg:flex-row">
        <aside className="lg:w-48">
          <nav className="flex gap-1 lg:flex-col">
            {items.map((item) => {
              const isActive = pathname.startsWith(item.to)
              return (
                <Link
                  key={item.to}
                  to={item.to}
                  className={cn(
                    buttonVariants({ variant: 'ghost' }),
                    'justify-start gap-2',
                    isActive
                      ? 'bg-muted hover:bg-muted'
                      : 'hover:bg-transparent hover:underline',
                  )}
                >
                  <item.icon className="size-4" />
                  {item.title}
                </Link>
              )
            })}
          </nav>
        </aside>
        <div className="flex-1">
          <Outlet />
        </div>
      </div>
    </div>
  )
}
