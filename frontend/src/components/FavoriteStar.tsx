import { useState } from 'react'
import { Star } from 'lucide-react'
import { isFavoriteService, toggleFavoriteService } from '../services/catalog.ts'

/** Star toggle for Favorites (per-user localStorage, see catalog.ts). */
export function FavoriteStar({ serviceId, serviceName }: { serviceId: string; serviceName: string }) {
  const [fav, setFav] = useState(() => isFavoriteService(serviceId))
  return (
    <button
      type="button"
      aria-label={fav ? `Remove ${serviceName} from favorites` : `Add ${serviceName} to favorites`}
      aria-pressed={fav}
      title={fav ? 'Remove from favorites' : 'Add to favorites'}
      onClick={(e) => {
        e.preventDefault()
        e.stopPropagation()
        setFav(toggleFavoriteService(serviceId))
      }}
      className="rounded-sm p-1 hover:bg-[#f2f3f3]"
    >
      <Star className={`h-4 w-4 ${fav ? 'fill-aws-orange text-aws-orange' : 'text-aws-muted'}`} aria-hidden="true" />
    </button>
  )
}
