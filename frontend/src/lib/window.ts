// Window controls for herd's own titlebar (the window is frameless on Linux
// and Windows; macOS keeps its traffic lights).

const rt = () => (window as any).runtime

export const minimise = () => rt()?.WindowMinimise()
export const toggleMaximise = () => rt()?.WindowToggleMaximise()
export const quit = () => rt()?.Quit()

export async function isMaximised(): Promise<boolean> {
  try {
    return !!(await rt()?.WindowIsMaximised())
  } catch {
    return false
  }
}
