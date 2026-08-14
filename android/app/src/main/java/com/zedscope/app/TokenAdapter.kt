package com.zedscope.app

import android.content.ClipData
import android.content.ClipboardManager
import android.view.LayoutInflater
import android.view.View
import android.view.ViewGroup
import android.widget.TextView
import android.widget.Toast
import androidx.recyclerview.widget.RecyclerView

/** Card-style adapter for extracted tokens. Self-contained: binds a List<Token>
 *  and copies the token value to the clipboard on tap. */
class TokenAdapter : RecyclerView.Adapter<TokenAdapter.VH>() {
    private var items: List<Token> = emptyList()
    fun setItems(list: List<Token>) { items = list; notifyDataSetChanged() }

    class VH(view: View) : RecyclerView.ViewHolder(view) {
        val tvKey: TextView = view.findViewById(R.id.tvKey)
        val tvSource: TextView = view.findViewById(R.id.tvSource)
        val tvValue: TextView = view.findViewById(R.id.tvValue)
        val tvLoginFlag: TextView = view.findViewById(R.id.tvLoginFlag)
    }

    override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): VH {
        val v = LayoutInflater.from(parent.context).inflate(R.layout.item_token, parent, false)
        return VH(v)
    }

    override fun onBindViewHolder(holder: VH, position: Int) {
        val t = items[position]
        holder.tvKey.text = t.key
        holder.tvSource.text = t.source
        holder.tvValue.text = t.value
        holder.tvLoginFlag.visibility = if (t.is_login) View.VISIBLE else View.GONE
        holder.itemView.setOnClickListener {
            val cm = holder.itemView.context.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as ClipboardManager
            cm.setPrimaryClip(ClipData.newPlainText(t.key, t.value))
            Toast.makeText(holder.itemView.context, "已复制 ${t.key}", Toast.LENGTH_SHORT).show()
        }
    }

    override fun getItemCount() = items.size
}
