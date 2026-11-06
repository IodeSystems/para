import { createFileRoute, redirect } from '@tanstack/react-router'

// Policy is part of areas now: a policy is an area that follows its source, and its exceptions,
// conventions, and constraints are annotations in the area's page. Old links land on the areas.
export const Route = createFileRoute('/_authed/policy')({
  beforeLoad: () => {
    throw redirect({ to: '/areas', replace: true })
  },
})
