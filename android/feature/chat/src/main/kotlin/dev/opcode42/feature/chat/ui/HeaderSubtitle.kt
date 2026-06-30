package dev.opcode42.feature.chat.ui

import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.BasicText
import androidx.compose.material3.Icon
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathParser
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.opcode42.core.design.text.StartEllipsisText
import dev.opcode42.core.design.text.fitStart
import dev.opcode42.core.design.text.homeRelativeDir
import dev.opcode42.core.design.theme.OnSurfaceFaint
import dev.opcode42.core.design.theme.Opcode42Mono

/**
 * The chat header subtitle: the session directory (home collapsed to `~`) and the current git
 * branch — e.g. `~/git/opcode42 · ⎇ main`. When the two don't fit, the shortfall is split fairly
 * (see [splitBudgets]) and both ellipsize at the START *together* (keeping each segment's tail),
 * rather than the path bearing the whole penalty. With no branch the path takes the full width.
 */
@Composable
internal fun HeaderSubtitle(
    directory: String,
    branch: String?,
    modifier: Modifier = Modifier,
) {
    val style = TextStyle(fontFamily = Opcode42Mono, fontSize = 11.5.sp, color = OnSurfaceFaint)
    val path = homeRelativeDir(directory)
    if (branch.isNullOrBlank()) {
        StartEllipsisText(text = path, style = style, modifier = modifier)
        return
    }
    val measurer = rememberTextMeasurer()
    val density = LocalDensity.current
    BoxWithConstraints(modifier) {
        val total = constraints.maxWidth
        val (pathText, branchText) = remember(path, branch, total, style) {
            // Fixed chrome between the two texts: spacer(5) + "·" + spacer(5) + icon(12) + spacer(3).
            val chrome = with(density) { (5 + 5 + 12 + 3).dp.toPx() }.toInt() +
                measurer.measure("·", style, softWrap = false, maxLines = 1).size.width
            val avail = (total - chrome).coerceAtLeast(0)
            val pathW = measurer.measure(path, style, softWrap = false, maxLines = 1).size.width
            val branchW = measurer.measure(branch, style, softWrap = false, maxLines = 1).size.width
            val (pathBudget, branchBudget) = splitBudgets(pathW, branchW, avail)
            // Both truncate from the START — keep each segment's most-specific tail (leaf dir, branch name).
            measurer.fitStart(path, style, pathBudget) to measurer.fitStart(branch, style, branchBudget)
        }
        Row(verticalAlignment = Alignment.CenterVertically) {
            BasicText(text = pathText, style = style, maxLines = 1, softWrap = false)
            Spacer(Modifier.width(5.dp))
            BasicText(text = "·", style = style, maxLines = 1, softWrap = false)
            Spacer(Modifier.width(5.dp))
            Icon(
                imageVector = GitBranchIcon,
                contentDescription = null,
                tint = OnSurfaceFaint,
                modifier = Modifier.size(12.dp),
            )
            Spacer(Modifier.width(3.dp))
            BasicText(text = branchText, style = style, maxLines = 1, softWrap = false)
        }
    }
}

/**
 * Fairly divide [avail] px between the path and the branch so both share any shortfall instead of
 * the path bearing it all. Each is granted up to half; whatever a shorter side leaves unused spills
 * to the longer one (so a short path never strands width). Returns (pathBudget, branchBudget) px.
 */
internal fun splitBudgets(pathW: Int, branchW: Int, avail: Int): Pair<Int, Int> {
    if (pathW + branchW <= avail) return pathW to branchW
    val half = avail / 2
    return when {
        pathW <= half -> pathW to (avail - pathW)
        branchW <= half -> (avail - branchW) to branchW
        else -> half to (avail - half)
    }
}

/**
 * Palantir Blueprint's 20px "git-branch" icon (resources/icons/20px/git-branch.svg). A filled
 * vector parsed straight from the SVG path; [Icon]'s `tint` recolors the fill, so the builder's
 * fill color is a placeholder.
 */
private val GitBranchIcon: ImageVector by lazy {
    ImageVector.Builder(
        name = "GitBranch",
        defaultWidth = 20.dp,
        defaultHeight = 20.dp,
        viewportWidth = 20f,
        viewportHeight = 20f,
    ).apply {
        addPath(
            pathData = PathParser().parsePathString(
                "M15 2c-1.66 0-3 1.34-3 3 0 1.3.84 2.4 2 2.82V9c0 1.1-.9 2-2 2H8c-.73 0-1.41.21-2 .55V5.82C7.16 5.4 8 4.3 8 3c0-1.66-1.34-3-3-3S2 1.34 2 3c0 1.3.84 2.4 2 2.82v8.37C2.84 14.6 2 15.7 2 17c0 1.66 1.34 3 3 3s3-1.34 3-3c0-1.25-.77-2.3-1.85-2.75C6.45 13.52 7.16 13 8 13h4c2.21 0 4-1.79 4-4V7.82C17.16 7.4 18 6.3 18 5c0-1.66-1.34-3-3-3M5 2c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1m0 16c-.55 0-1-.45-1-1s.45-1 1-1 1 .45 1 1-.45 1-1 1M15 6c-.55 0-1-.45-1-1s.45-1 1-1 1 .45 1 1-.45 1-1 1",
            ).toNodes(),
            fill = SolidColor(Color.Black),
        )
    }.build()
}
