import { Navigate, Route, Routes } from 'react-router-dom'
import Login from './Login'
import { useAuth } from './auth'
import { Layout } from '@/components/layout'
import { SettingsLayout } from '@/components/settings-layout'
import {
  DashboardPage,
  MissionsPage,
  NotFoundPage,
  RemoteControlPage,
  SettingsAgentGroupsPage,
  SettingsAgentsPage,
  SettingsLayoutsPage,
  TasksPage,
  VideoStreamPage,
} from './pages'
import { Monitoring } from './pages/monitoring/Monitoring'
import { ContractHealthPage } from './pages/telemetry/ContractHealthPage'
import { TelemetryIndexPage } from './pages/telemetry/TelemetryIndexPage'
import { TelemetryLayout } from './pages/telemetry/TelemetryLayout'
import { TelemetryLivePage } from './pages/telemetry/TelemetryLivePage'
import { TelemetryReplayPage } from './pages/telemetry/TelemetryReplayPage'

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
        <Route path="video-stream" element={<VideoStreamPage />} />
        <Route path="remote-control" element={<RemoteControlPage />} />
        <Route path="telemetry">
          <Route index element={<TelemetryIndexPage />} />
          <Route path=":agentId" element={<TelemetryLayout />}>
            <Route index element={<TelemetryLivePage />} />
            <Route path="contract" element={<ContractHealthPage />} />
            <Route path="replay" element={<TelemetryReplayPage />} />
          </Route>
        </Route>
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
