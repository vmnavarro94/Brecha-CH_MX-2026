import './App.css'
import { useMarketSocket } from './hooks/useMarketSocket'
import Shell from './Shell'
import StatusBar from './components/StatusBar'
import PriceTable from './components/PriceTable'
import SpreadChart from './components/SpreadChart'
import PnLChart from './components/PnLChart'
import OpportunityFeed from './components/OpportunityFeed'
import TradeHistory from './components/TradeHistory'

export default function App() {
  useMarketSocket()

  return (
    <Shell>
      <StatusBar />
      <div className="bx-content">
        <div className="bx-cockpit">
          <div className="bx-col">
            <SpreadChart />
            <PnLChart />
          </div>
          <div className="bx-col">
            <PriceTable />
            <OpportunityFeed />
          </div>
        </div>
        <div className="bx-fullrow">
          <TradeHistory />
        </div>
      </div>
    </Shell>
  )
}
