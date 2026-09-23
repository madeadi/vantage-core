import { Link, useLocation } from 'react-router-dom'
import { Activity, Gamepad2, LayoutDashboard, Radio, Settings, Video } from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'

const nav = [
  { title: 'Dashboard', to: '/', icon: LayoutDashboard },
  { title: 'Layout Monitoring', to: '/monitoring', icon: Activity },
  { title: 'Telemetry', to: '/telemetry', icon: Radio },
  { title: 'Video Stream', to: '/video-stream', icon: Video },
  { title: 'Remote Control', to: '/remote-control', icon: Gamepad2 },
  { title: 'Settings', to: '/settings', icon: Settings },
]

export function AppSidebar() {
  const { pathname } = useLocation()

  return (
    <Sidebar>
      <SidebarHeader>
        <div className="flex items-center gap-2 px-2 py-1.5">
          <div className="flex size-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <span className="text-sm font-bold">V</span>
          </div>
          <span className="text-sm font-semibold tracking-wide uppercase">
            VantageOS
          </span>
        </div>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupLabel>Navigation</SidebarGroupLabel>
          <SidebarMenu>
            {nav.map((item) => {
              const isActive =
                item.to === '/'
                  ? pathname === '/'
                  : pathname.startsWith(item.to)
              return (
                <SidebarMenuItem key={item.to}>
                  <SidebarMenuButton asChild isActive={isActive} tooltip={item.title}>
                    <Link to={item.to}>
                      <item.icon />
                      <span>{item.title}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )
            })}
          </SidebarMenu>
        </SidebarGroup>
      </SidebarContent>
    </Sidebar>
  )
}
