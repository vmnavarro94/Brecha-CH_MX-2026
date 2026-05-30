import './App.css'
import { useMarketSocket } from './hooks/useMarketSocket'
import Shell from './Shell'
import StatusBar from './components/StatusBar'
import PriceTable from './components/PriceTable'
import SpreadChart from './components/SpreadChart'
import PnLChart from './components/PnLChart'
import OpportunityFeed from './components/OpportunityFeed'
import TradeHistory from './components/TradeHistory'
import TweaksPanel from './components/TweaksPanel'
import SpreadHeatmap from './components/SpreadHeatmap'
import TopOpportunityBanner from './components/TopOpportunityBanner'
import ReconnectBanner from './components/ReconnectBanner'
import PerPairPnL from './components/PerPairPnL'
import StrategyPnL from './components/StrategyPnL'

export default function App() {
  useMarketSocket()

  return (
    <Shell>
      <StatusBar />
      <div className="bx-content">
        <ReconnectBanner />
        <TopOpportunityBanner />
        <div className="bx-cockpit">
          <div className="bx-grid-main">
            <div className="bx-area bx-area--chart"><SpreadChart /></div>
            <div className="bx-area bx-area--prices"><PriceTable /></div>
            <div className="bx-area bx-area--pnl"><PnLChart /></div>
            <div className="bx-area bx-area--heat"><SpreadHeatmap /></div>
            <div className="bx-area bx-area--feed"><OpportunityFeed /></div>
          </div>
          <div className="bx-area bx-area--tweaks"><TweaksPanel /></div>
        </div>
        <div className="bx-fullrow">
          <PerPairPnL />
        </div>
        <div className="bx-fullrow">
          <StrategyPnL />
        </div>
        <div className="bx-fullrow">
          <TradeHistory />
        </div>
      </div>
    </Shell>
  )
}
