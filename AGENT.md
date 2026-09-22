# Vantage OS Core

- When creating an HTTP API, please generate swagger comment using swaggo/swag. whenever you add or change a handler, re-run swag init to regenerate the docs
- For interaction between robot and core, read core swagger API at /docs.

Vantage OS is a Fleet Management System to provide seamless management and control of robotic fleets.

# Features
- Agent/Robot registration and authentication
- Task dispatching and management
- Mission management
- Command and Control UI
- Telemetry and Pose reporting
- Video streaming
- Incident reporting and management
- Remote control 

# UI

UI in `/ui` folder contains web app for admin panel. Admin panel is used to manage core functionalities. 

The stack for the web app is:
- React
- Typescript
- Tailwind
- Shadcn