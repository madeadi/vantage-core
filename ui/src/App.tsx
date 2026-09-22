import { Navigate, Route, Routes } from 'react-router-dom'
import Login from './Login'
import { useAuth } from './auth'
import { Layout } from '@/components/layout'
import { SettingsLayout } from '@/components/settings-layout'
import {
  DashboardPage,
  MissionsPage,
  NotFoundPage,
  SettingsAgentGroupsPage,
  SettingsAgentsPage,
  SettingsLayoutsPage,
  TasksPage,
} from './pages'
import { Monitoring } from './pages/monitoring/Monitoring'

function App() {
  const { isValid } = useAuth()

  if (!isValid) {
    return <Login />
  }

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<DashboardPage />} />
        <Route path="missions" element={<MissionsPage />} />
        <Route path="tasks" element={<TasksPage />} />
        <Route path="monitoring" element={<Monitoring />} />
        <Route path="settings" element={<SettingsLayout />}>
          <Route index element={<Navigate to="/settings/agents" replace />} />
          <Route path="agents" element={<SettingsAgentsPage />} />
          <Route path="agent-groups" element={<SettingsAgentGroupsPage />} />
          <Route path="layouts" element={<SettingsLayoutsPage />} />
        </Route>
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

export default App
