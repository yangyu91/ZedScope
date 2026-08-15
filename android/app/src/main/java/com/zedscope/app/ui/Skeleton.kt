package com.zedscope.app.ui

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Shader
import android.util.AttributeSet
import android.view.View
import android.view.ViewOutlineProvider
import android.view.animation.LinearInterpolator
import android.widget.FrameLayout
import androidx.core.content.ContextCompat
import com.zedscope.app.R

/**
 * A single, self-contained shimmer placeholder block (no external library).
 *
 * Draws a rounded base fill and a soft highlight that sweeps left to right on a
 * loop, reading as the "precise skeleton" the product uses for every async
 * load (capture log, token list, ...). The animation pauses when detached and
 * resumes when attached, so no handler leaks.
 */
class SkeletonView @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
    defStyle: Int = 0
) : View(context, attrs, defStyle) {

    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rect = RectF()
    private val radius = resources.getDimension(R.dimen.yami_radius_md)
    private val base = ContextCompat.getColor(context, R.color.yami_surface_2)
    private val hi = ContextCompat.getColor(context, R.color.yami_on_accent_soft)
    private var sweep = 0f
    private var progress = 0f

    private val anim = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = 1100L
        interpolator = LinearInterpolator()
        repeatCount = ValueAnimator.INFINITE
        repeatMode = ValueAnimator.RESTART
        addUpdateListener {
            progress = (it?.animatedValue as? Float) ?: 0f
            invalidate()
        }
    }

    init { if (!anim.isRunning) anim.start() }

    override fun onSizeChanged(w: Int, h: Int, oldw: Int, oldh: Int) {
        super.onSizeChanged(w, h, oldw, oldh)
        rect.set(0f, 0f, w.toFloat(), h.toFloat())
        sweep = w * 0.7f
    }

    override fun onDraw(canvas: Canvas) {
        val w = width.toFloat()
        val h = height.toFloat()
        paint.shader = null
        paint.color = base
        canvas.drawRoundRect(rect, radius, radius, paint)
        // 105°-ish diagonal neon-cyan micro-shimmer (ret2shell feel)
        val start = -sweep + progress * (w + sweep)
        val grad = LinearGradient(
            start, h, start + sweep * 0.5f, 0f,
            intArrayOf(base, hi, base),
            floatArrayOf(0f, 0.5f, 1f),
            Shader.TileMode.CLAMP
        )
        paint.shader = grad
        canvas.drawRoundRect(rect, radius, radius, paint)
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        if (!anim.isRunning) anim.start()
    }

    override fun onDetachedFromWindow() {
        anim.cancel()
        super.onDetachedFromWindow()
    }
}

/**
 * Three staggered dots shown while the AI is "thinking" — the calm equivalent
 * of a skeleton for a streaming conversation (GitHub / Claude use the same cue).
 */
class TypingDots @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
    defStyle: Int = 0
) : FrameLayout(context, attrs, defStyle) {

    private val dots = ArrayList<View>(3)
    private val animators = ArrayList<ValueAnimator>(3)

    init {
        val dp = resources.displayMetrics.density
        val size = (8 * dp).toInt()
        val gap = (6 * dp).toInt()
        val color = ContextCompat.getColor(context, R.color.yami_accent)
        repeat(3) {
            val d = View(context).apply {
                setBackgroundColor(color)
                layoutParams = LayoutParams(size, size)
                clipToOutline = true
                outlineProvider = object : ViewOutlineProvider() {
                    override fun getOutline(view: View, outline: android.graphics.Outline) {
                        outline.setOval(0, 0, view.width, view.height)
                    }
                }
            }
            addView(d)
            dots.add(d)
        }
        post {
            val total = 3 * size + 2 * gap
            val left = (width - total) / 2f
            dots.forEachIndexed { i, v -> v.x = left + i * (size + gap) }
        }
    }

    fun start() {
        stop()
        dots.forEachIndexed { i, v ->
            val a = ValueAnimator.ofFloat(0.3f, 1f).apply {
                duration = 520L
                repeatCount = ValueAnimator.INFINITE
                repeatMode = ValueAnimator.REVERSE
                startDelay = i * 140L
                addUpdateListener { v.alpha = (it?.animatedValue as? Float) ?: 1f }
            }
            a.start()
            animators.add(a)
        }
    }

    fun stop() {
        animators.forEach { it.cancel() }
        animators.clear()
        dots.forEach { it.alpha = 1f }
    }

    override fun onDetachedFromWindow() {
        stop()
        super.onDetachedFromWindow()
    }
}
