import { CONNECTION_STAGES } from '../../lib/stores/connectionStore';
import type { ConnectionDashboardState, ConnectionStage } from '../../lib/stores/connectionStore';

const LABELS: Record<ConnectionStage, string> = {
  dns: 'DNS',
  vk: 'VK',
  turn: 'TURN',
  wrap: 'WRAP',
  dtls: 'DTLS',
  workers: 'Потоки',
  vpn: 'VPN',
};

export default function ConnectionProgress({ connection }: { connection: ConnectionDashboardState }) {
  return (
    <section className="connection-progress" aria-label="Этапы подключения">
      <div className="connection-progress-track">
        {CONNECTION_STAGES.map((stage, index) => {
          const status = connection.stages[stage];
          return (
            <div className="progress-segment" key={stage}>
              <div className={`progress-step progress-step--${status}`}>
                <span className="progress-dot">{status === 'success' ? '✓' : status === 'error' ? '!' : ''}</span>
                <span className="progress-label">{LABELS[stage]}</span>
              </div>
              {index < CONNECTION_STAGES.length - 1 && (
                <span className={`progress-line${status === 'success' ? ' progress-line--done' : ''}`} />
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
