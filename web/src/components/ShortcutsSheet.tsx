import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['d'], 'Dashboard'], [['i'], 'Invoices'], [['c'], 'Clients'], [['p'], 'Payments'], [['m'], 'Month-end'], [['t'], 'Time tracking'], [['e'], 'Expenses'], [['r'], 'Reports'], [[','], 'Settings']] },
  { title: 'Create', keys: [[['n'], 'New invoice'], [['a'], 'New client']] },
  { title: 'Search', keys: [[['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close a dialog, a menu or Settings']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">Search finds invoices by number, client or PO, and clients by name or contact.</p>
    </Modal>
  )
}
